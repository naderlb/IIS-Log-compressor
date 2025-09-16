package main

import (
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/smtp"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const toolName = "IIS Log compressor by Nader Barakat . www.naderb.org tools"

// Config holds all configuration settings
type Config struct {
	SourceFolder                string      `json:"source_folder"`
	DestFolder                  string      `json:"dest_folder"`
	LogAgeDays                  int         `json:"log_age_days"`
	RetentionDays               int         `json:"retention_days"`
	CleanupOldLogs              bool        `json:"cleanup_old_logs"`
	DeleteOriginalAfterCompress bool        `json:"delete_original_after_compress"`
	CompressCurrentMonth        bool        `json:"compress_current_month"`
	ArchiveScope                string      `json:"archive_scope"`
	KeepLastNArchives           int         `json:"keep_last_n_archives"`
	DestFileNamePattern         string      `json:"dest_file_name_pattern"`
	CompressionType             string      `json:"compression_type"`
	MaxCPUs                     int         `json:"max_cpus"`
	EmailNotification           EmailConfig `json:"email_notification"`
}

// EmailConfig holds email notification settings
type EmailConfig struct {
	Enabled  bool   `json:"enabled"`
	SMTPHost string `json:"smtp_host"`
	SMTPPort int    `json:"smtp_port"`
	Username string `json:"username"`
	Password string `json:"password"`
	From     string `json:"from"`
	To       string `json:"to"`
	Subject  string `json:"subject"`
}

// CompressionStats holds statistics about compression operations
type CompressionStats struct {
	FilesProcessed  int
	FilesCompressed int
	TotalSizeBefore int64
	TotalSizeAfter  int64
	Errors          []string
	StartTime       time.Time
	EndTime         time.Time
	EmailStatus     string
	GroupCount      int
}

// LogFile represents a log file to be processed
type LogFile struct {
	Path    string
	Size    int64
	ModTime time.Time
}

var (
	config Config
	stats  CompressionStats
	mu     sync.Mutex
)

func main() {
	fmt.Println(toolName)
	fmt.Println("========================")

	// Load configuration
	if err := loadConfig("config.json"); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Set CPU limit
	if config.MaxCPUs > 0 && config.MaxCPUs <= runtime.NumCPU() {
		runtime.GOMAXPROCS(config.MaxCPUs)
	} else {
		runtime.GOMAXPROCS(runtime.NumCPU())
	}

	fmt.Printf("Using %d CPUs for processing\n", runtime.GOMAXPROCS(0))

	// Initialize stats
	stats = CompressionStats{
		StartTime: time.Now(),
		Errors:    make([]string, 0),
	}

	// Process logs
	if err := processLogs(); err != nil {
		log.Printf("Error processing logs: %v", err)
		stats.Errors = append(stats.Errors, err.Error())
	}

	// Cleanup old compressed logs if enabled
	if config.CleanupOldLogs {
		if err := cleanupOldCompressedLogs(); err != nil {
			log.Printf("Error cleaning up old logs: %v", err)
			stats.Errors = append(stats.Errors, err.Error())
		}
	}

	// Finalize stats
	stats.EndTime = time.Now()

	// Print summary
	printSummary()

	// Send email notification if enabled
	if config.EmailNotification.Enabled {
		if err := sendEmailNotification(); err != nil {
			stats.EmailStatus = fmt.Sprintf("Email send failed: %v", err)
			log.Printf("Failed to send email notification: %v", err)
		} else {
			stats.EmailStatus = "Email sent successfully"
		}
	} else {
		stats.EmailStatus = "Email disabled"
	}

	// Write run report next to exe
	if err := writeRunReport(); err != nil {
		log.Printf("Failed to write run report: %v", err)
	}
}

func loadConfig(filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("failed to read config file: %v", err)
	}

	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse config file: %v", err)
	}

	// Validate configuration
	if config.SourceFolder == "" {
		return fmt.Errorf("source_folder is required")
	}
	if config.DestFolder == "" {
		return fmt.Errorf("dest_folder is required")
	}
	if config.LogAgeDays <= 0 {
		config.LogAgeDays = 7 // Default to 7 days
	}
	if config.RetentionDays < 0 {
		config.RetentionDays = 0
	}
	if config.CompressionType == "" {
		config.CompressionType = "zip" // Default to zip
	}
	if config.DestFileNamePattern == "" {
		config.DestFileNamePattern = "logs_%Y%m%d_%H%M%S" // Default pattern
	}
	if config.ArchiveScope == "" {
		config.ArchiveScope = "monthly" // monthly or daily
	}
	if strings.ToLower(config.ArchiveScope) != "monthly" && strings.ToLower(config.ArchiveScope) != "daily" {
		config.ArchiveScope = "monthly"
	}
	if config.KeepLastNArchives < 0 {
		config.KeepLastNArchives = 0
	}

	return nil
}

func processLogs() error {
	// Create destination folder if it doesn't exist
	if err := os.MkdirAll(config.DestFolder, 0755); err != nil {
		return fmt.Errorf("failed to create destination folder: %v", err)
	}

	// Find log files
	logFiles, err := findLogFiles()
	if err != nil {
		return fmt.Errorf("failed to find log files: %v", err)
	}

	if len(logFiles) == 0 {
		fmt.Println("No log files found matching criteria")
		return nil
	}

	fmt.Printf("Found %d log files to process\n", len(logFiles))

	// Group files by scope
	groups := make(map[string][]LogFile)
	for _, lf := range logFiles {
		key := groupKeyForTime(lf.ModTime)
		groups[key] = append(groups[key], lf)
	}
	// Exclude current period if configured (for monthly) or by default for daily (exclude today)
	nowKey := groupKeyForTime(time.Now())
	if strings.ToLower(config.ArchiveScope) == "monthly" {
		if !config.CompressCurrentMonth {
			delete(groups, nowKey)
		}
	} else { // daily
		// Always exclude today for safety unless user later asks otherwise
		delete(groups, nowKey)
	}
	stats.GroupCount = len(groups)

	// Compress each group in parallel
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, runtime.GOMAXPROCS(0))
	for gk, files := range groups {
		gk := gk
		files := files
		wg.Add(1)
		go func() {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			if err := compressMonthGroup(gk, files); err != nil {
				mu.Lock()
				stats.Errors = append(stats.Errors, fmt.Sprintf("Error compressing group %s: %v", gk, err))
				mu.Unlock()
			}
		}()
	}

	wg.Wait()
	return nil
}

// groupKeyForTime returns grouping key based on ArchiveScope
func groupKeyForTime(t time.Time) string {
	if strings.ToLower(config.ArchiveScope) == "daily" {
		return t.Format("2006-01-02")
	}
	return t.Format("2006-01")
}

// generateArchiveFileName builds archive name from reference time and scope
func generateArchiveFileName(ref time.Time) string {
	pattern := config.DestFileNamePattern
	if strings.ToLower(config.ArchiveScope) == "daily" {
		pattern = strings.ReplaceAll(pattern, "%Y", ref.Format("2006"))
		pattern = strings.ReplaceAll(pattern, "%m", ref.Format("01"))
		pattern = strings.ReplaceAll(pattern, "%d", ref.Format("02"))
		pattern = strings.ReplaceAll(pattern, "%H", "00")
		pattern = strings.ReplaceAll(pattern, "%M", "00")
		pattern = strings.ReplaceAll(pattern, "%S", "00")
		pattern = strings.ReplaceAll(pattern, "%y", ref.Format("06"))
		pattern = strings.ReplaceAll(pattern, "%j", ref.Format("002"))
	} else {
		pattern = strings.ReplaceAll(pattern, "%Y", ref.Format("2006"))
		pattern = strings.ReplaceAll(pattern, "%m", ref.Format("01"))
		pattern = strings.ReplaceAll(pattern, "%d", "01")
		pattern = strings.ReplaceAll(pattern, "%H", "00")
		pattern = strings.ReplaceAll(pattern, "%M", "00")
		pattern = strings.ReplaceAll(pattern, "%S", "00")
		pattern = strings.ReplaceAll(pattern, "%y", ref.Format("06"))
		pattern = strings.ReplaceAll(pattern, "%j", ref.Format("002"))
	}
	return pattern + getCompressionExtension()
}

// compressMonthGroup creates a single archive for all files in a given group key (month or day)
func compressMonthGroup(groupKey string, files []LogFile) error {
	if len(files) == 0 {
		return nil
	}

	ref := files[0].ModTime
	destFileName := generateArchiveFileName(ref)
	destPath := filepath.Join(config.DestFolder, destFileName)

	// Create destination file
	destFile, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %v", err)
	}

	switch strings.ToLower(config.CompressionType) {
	case "zip":
		added, err := addFilesToZip(destFile, files, destPath)
		if err != nil {
			_ = destFile.Close()
			_ = os.Remove(destPath)
			return err
		}
		// Close file to flush
		if err := destFile.Close(); err != nil {
			return fmt.Errorf("closing destination file: %v", err)
		}
		// Verify zip content before any deletion
		verified := verifyZipContainsAll(destPath, added)
		if config.DeleteOriginalAfterCompress {
			for path, ok := range verified {
				if ok {
					if err := deleteWithRetry(path, 3, 500*time.Millisecond); err != nil {
						fmt.Printf("Warning: Failed to remove original file %s: %v\n", path, err)
					}
				}
			}
		}
		// Update compressed size
		if info, err := os.Stat(destPath); err == nil {
			mu.Lock()
			stats.TotalSizeAfter += info.Size()
			mu.Unlock()
		}
	case "gzip":
		_ = destFile.Close()
		_ = os.Remove(destPath)
		return fmt.Errorf("grouped mode requires zip compression; gzip not supported for grouped archive")
	default:
		_ = destFile.Close()
		_ = os.Remove(destPath)
		return fmt.Errorf("unsupported compression type: %s (supported: zip)", config.CompressionType)
	}

	return nil
}

func findLogFiles() ([]LogFile, error) {
	var logFiles []LogFile
	cutoffDate := time.Now().AddDate(0, 0, -config.LogAgeDays)

	err := filepath.Walk(config.SourceFolder, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Check if file is old enough
		if info.ModTime().After(cutoffDate) {
			return nil
		}

		// Check if it's a log file (common IIS log extensions)
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".log" || ext == ".txt" || strings.Contains(strings.ToLower(path), "log") {
			logFiles = append(logFiles, LogFile{
				Path:    path,
				Size:    info.Size(),
				ModTime: info.ModTime(),
			})
		}

		return nil
	})

	// Sort by modification time (oldest first)
	sort.Slice(logFiles, func(i, j int) bool {
		return logFiles[i].ModTime.Before(logFiles[j].ModTime)
	})

	return logFiles, err
}

// addFilesToZip writes all files into zip and returns the list of successfully added file paths
func addFilesToZip(destFile *os.File, files []LogFile, destPath string) ([]string, error) {
	zipWriter := zip.NewWriter(destFile)
	added := make([]string, 0, len(files))
	for _, lf := range files {
		// Open source
		srcFile, err := os.Open(lf.Path)
		if err != nil {
			fmt.Printf("Warning: failed to open %s: %v\n", lf.Path, err)
			mu.Lock()
			stats.Errors = append(stats.Errors, fmt.Sprintf("open %s: %v", lf.Path, err))
			mu.Unlock()
			continue
		}
		entryName := filepath.Base(lf.Path)
		zw, err := zipWriter.Create(entryName)
		if err != nil {
			_ = srcFile.Close()
			fmt.Printf("Warning: failed to create zip entry for %s: %v\n", lf.Path, err)
			mu.Lock()
			stats.Errors = append(stats.Errors, fmt.Sprintf("zip entry %s: %v", lf.Path, err))
			mu.Unlock()
			continue
		}
		if _, err := io.Copy(zw, srcFile); err != nil {
			_ = srcFile.Close()
			fmt.Printf("Warning: failed to copy %s into zip: %v\n", lf.Path, err)
			mu.Lock()
			stats.Errors = append(stats.Errors, fmt.Sprintf("zip copy %s: %v", lf.Path, err))
			mu.Unlock()
			continue
		}
		_ = srcFile.Close()

		mu.Lock()
		stats.FilesProcessed++
		stats.FilesCompressed++
		stats.TotalSizeBefore += lf.Size
		mu.Unlock()

		fmt.Printf("Added to %s: %s\n", destPath, lf.Path)
		added = append(added, lf.Path)
	}
	if err := zipWriter.Close(); err != nil {
		return added, fmt.Errorf("closing zip writer: %v", err)
	}
	return added, nil
}

// verifyZipContainsAll checks that each path in added exists in the zip and uncompressed size matches
func verifyZipContainsAll(zipPath string, added []string) map[string]bool {
	result := make(map[string]bool, len(added))
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		for _, p := range added {
			result[p] = false
		}
		stats.Errors = append(stats.Errors, fmt.Sprintf("verify open zip %s: %v", zipPath, err))
		return result
	}
	defer zr.Close()

	// Build map of entry name to uncompressed size
	entries := make(map[string]uint64, len(zr.File))
	for _, f := range zr.File {
		entries[f.Name] = f.UncompressedSize64
	}
	for _, p := range added {
		base := filepath.Base(p)
		stat, err := os.Stat(p)
		if err != nil {
			result[p] = false
			continue
		}
		size := stat.Size()
		if u, ok := entries[base]; ok && int64(u) == size {
			result[p] = true
		} else {
			result[p] = false
		}
	}
	return result
}

func compressLogFile(logFile LogFile) error {
	mu.Lock()
	stats.FilesProcessed++
	stats.TotalSizeBefore += logFile.Size
	mu.Unlock()

	// Generate destination filename
	destFileName := generateDestFileName(logFile)
	destPath := filepath.Join(config.DestFolder, destFileName)

	// Create destination file
	destFile, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %v", err)
	}
	defer destFile.Close()

	// Open source file
	srcFile, err := os.Open(logFile.Path)
	if err != nil {
		return fmt.Errorf("failed to open source file: %v", err)
	}
	defer srcFile.Close()

	// Compress based on type
	var compressedSize int64
	switch strings.ToLower(config.CompressionType) {
	case "zip":
		compressedSize, err = compressZip(srcFile, destFile, filepath.Base(logFile.Path))
	case "gzip":
		compressedSize, err = compressGzip(srcFile, destFile)
	default:
		return fmt.Errorf("unsupported compression type: %s (supported: zip, gzip)", config.CompressionType)
	}

	if err != nil {
		os.Remove(destPath) // Clean up failed compression
		return fmt.Errorf("compression failed: %v", err)
	}

	// Update stats
	mu.Lock()
	stats.FilesCompressed++
	stats.TotalSizeAfter += compressedSize
	mu.Unlock()

	fmt.Printf("Compressed: %s -> %s (%.2f%% reduction)\n",
		logFile.Path, destPath,
		float64(logFile.Size-compressedSize)/float64(logFile.Size)*100)

	// Remove original file after successful compression (per-file mode)
	if config.DeleteOriginalAfterCompress {
		if err := os.Remove(logFile.Path); err != nil {
			fmt.Printf("Warning: Failed to remove original file %s: %v\n", logFile.Path, err)
		}
	}

	return nil
}

func compressZip(srcFile *os.File, destFile *os.File, fileName string) (int64, error) {
	zipWriter := zip.NewWriter(destFile)

	fileWriter, err := zipWriter.Create(fileName)
	if err != nil {
		return 0, err
	}

	if _, err = io.Copy(fileWriter, srcFile); err != nil {
		_ = zipWriter.Close()
		return 0, err
	}

	if err := zipWriter.Close(); err != nil {
		return 0, err
	}

	info, err := destFile.Stat()
	if err != nil {
		return 0, err
	}

	return info.Size(), nil
}

func compressGzip(srcFile *os.File, destFile *os.File) (int64, error) {
	gzipWriter := gzip.NewWriter(destFile)

	if _, err := io.Copy(gzipWriter, srcFile); err != nil {
		_ = gzipWriter.Close()
		return 0, err
	}

	if err := gzipWriter.Close(); err != nil {
		return 0, err
	}

	info, err := destFile.Stat()
	if err != nil {
		return 0, err
	}

	return info.Size(), nil
}

func generateDestFileName(logFile LogFile) string {
	now := time.Now()
	pattern := config.DestFileNamePattern
	baseName := strings.TrimSuffix(filepath.Base(logFile.Path), filepath.Ext(logFile.Path))

	// Replace placeholders
	pattern = strings.ReplaceAll(pattern, "%Y", now.Format("2006"))
	pattern = strings.ReplaceAll(pattern, "%m", now.Format("01"))
	pattern = strings.ReplaceAll(pattern, "%d", now.Format("02"))
	pattern = strings.ReplaceAll(pattern, "%H", now.Format("15"))
	pattern = strings.ReplaceAll(pattern, "%M", now.Format("04"))
	pattern = strings.ReplaceAll(pattern, "%S", now.Format("05"))
	pattern = strings.ReplaceAll(pattern, "%y", now.Format("06"))
	pattern = strings.ReplaceAll(pattern, "%j", strconv.Itoa(now.YearDay()))
	pattern = strings.ReplaceAll(pattern, "%F", baseName)

	if !strings.Contains(config.DestFileNamePattern, "%F") {
		pattern = pattern + "_" + baseName
	}

	// Add compression extension
	ext := getCompressionExtension()
	return pattern + ext
}

// generateMonthlyDestFileName builds a per-month archive name from a reference time
func generateMonthlyDestFileName(ref time.Time) string {
	pattern := config.DestFileNamePattern
	pattern = strings.ReplaceAll(pattern, "%Y", ref.Format("2006"))
	pattern = strings.ReplaceAll(pattern, "%m", ref.Format("01"))
	pattern = strings.ReplaceAll(pattern, "%d", "01")
	pattern = strings.ReplaceAll(pattern, "%H", "00")
	pattern = strings.ReplaceAll(pattern, "%M", "00")
	pattern = strings.ReplaceAll(pattern, "%S", "00")
	pattern = strings.ReplaceAll(pattern, "%y", ref.Format("06"))
	pattern = strings.ReplaceAll(pattern, "%j", ref.Format("002"))
	pattern = strings.ReplaceAll(pattern, "%F", "logs")
	if !strings.Contains(pattern, "2006") && !strings.Contains(pattern, "06") && !strings.Contains(pattern, "01") {
		pattern = pattern + "_" + ref.Format("2006_01")
	}
	return pattern + getCompressionExtension()
}

func getCompressionExtension() string {
	switch strings.ToLower(config.CompressionType) {
	case "zip":
		return ".zip"
	case "gzip":
		return ".gz"
	default:
		return ".zip"
	}
}

func cleanupOldCompressedLogs() error {
	// Option A: Keep last N archives if set
	if config.KeepLastNArchives > 0 {
		entries, err := os.ReadDir(config.DestFolder)
		if err != nil {
			return err
		}
		type fileInfo struct {
			path string
			mod  time.Time
		}
		var files []fileInfo
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			p := filepath.Join(config.DestFolder, e.Name())
			if !strings.HasSuffix(strings.ToLower(e.Name()), ".zip") && !strings.HasSuffix(strings.ToLower(e.Name()), ".gz") {
				continue
			}
			fi, err := os.Stat(p)
			if err != nil {
				continue
			}
			files = append(files, fileInfo{path: p, mod: fi.ModTime()})
		}
		sort.Slice(files, func(i, j int) bool { return files[i].mod.After(files[j].mod) })
		for idx, f := range files {
			if idx >= config.KeepLastNArchives {
				fmt.Printf("Removing old compressed log (keep last %d): %s\n", config.KeepLastNArchives, f.path)
				_ = os.Remove(f.path)
			}
		}
		return nil
	}

	// Option B: Retention by age
	if config.RetentionDays <= 0 {
		return nil
	}
	cutoffDate := time.Now().AddDate(0, 0, -config.RetentionDays)
	return filepath.Walk(config.DestFolder, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if info.ModTime().Before(cutoffDate) {
			fmt.Printf("Removing old compressed log: %s\n", path)
			return os.Remove(path)
		}
		return nil
	})
}

func printSummary() {
	fmt.Println("\n" + strings.Repeat("=", 50))
	fmt.Println("COMPRESSION SUMMARY")
	fmt.Println(strings.Repeat("=", 50))
	fmt.Printf("Files processed: %d\n", stats.FilesProcessed)
	fmt.Printf("Files compressed: %d\n", stats.FilesCompressed)
	fmt.Printf("Total size before: %.2f MB\n", float64(stats.TotalSizeBefore)/(1024*1024))
	fmt.Printf("Total size after: %.2f MB\n", float64(stats.TotalSizeAfter)/(1024*1024))

	if stats.TotalSizeBefore > 0 {
		reduction := float64(stats.TotalSizeBefore-stats.TotalSizeAfter) / float64(stats.TotalSizeBefore) * 100
		fmt.Printf("Compression ratio: %.2f%%\n", reduction)
	}

	fmt.Printf("Processing time: %v\n", stats.EndTime.Sub(stats.StartTime))

	if len(stats.Errors) > 0 {
		fmt.Printf("Errors encountered: %d\n", len(stats.Errors))
		for i, err := range stats.Errors {
			if i < 5 { // Show first 5 errors
				fmt.Printf("  - %s\n", err)
			}
		}
		if len(stats.Errors) > 5 {
			fmt.Printf("  ... and %d more errors\n", len(stats.Errors)-5)
		}
	}
	fmt.Println(strings.Repeat("=", 50))
}

// sendEmailNotification includes tool branding in subject
func sendEmailNotification() error {
	// Determine status
	status := "Success"
	if len(stats.Errors) > 0 {
		status = "Failed"
	}

	// Hostname
	host, _ := os.Hostname()
	if host == "" {
		host = "unknown-host"
	}

	// Subject: e.g., "Success - IIS backup myhost.domain"
	subject := fmt.Sprintf("%s - IIS backup %s", status, host)
	if s := strings.TrimSpace(config.EmailNotification.Subject); s != "" {
		// If user provided a custom subject, still prefix with status + IIS backup + host
		subject = fmt.Sprintf("%s - IIS backup %s - %s", status, host, s)
	}

	// HTML body
	elapsed := stats.EndTime.Sub(stats.StartTime)
	reduction := 0.0
	if stats.TotalSizeBefore > 0 {
		reduction = float64(stats.TotalSizeBefore-stats.TotalSizeAfter) / float64(stats.TotalSizeBefore) * 100
	}
	body := strings.Builder{}
	body.WriteString("<html><body>")
	body.WriteString(fmt.Sprintf("<h3>%s</h3>", subject))
	body.WriteString("<table border=\"1\" cellpadding=\"6\" cellspacing=\"0\">")
	body.WriteString(fmt.Sprintf("<tr><td>Groups (months)</td><td>%d</td></tr>", stats.GroupCount))
	body.WriteString(fmt.Sprintf("<tr><td>Files processed</td><td>%d</td></tr>", stats.FilesProcessed))
	body.WriteString(fmt.Sprintf("<tr><td>Files compressed</td><td>%d</td></tr>", stats.FilesCompressed))
	body.WriteString(fmt.Sprintf("<tr><td>Total before</td><td>%.2f MB</td></tr>", float64(stats.TotalSizeBefore)/(1024*1024)))
	body.WriteString(fmt.Sprintf("<tr><td>Total after</td><td>%.2f MB</td></tr>", float64(stats.TotalSizeAfter)/(1024*1024)))
	body.WriteString(fmt.Sprintf("<tr><td>Compression ratio</td><td>%.2f%%</td></tr>", reduction))
	body.WriteString(fmt.Sprintf("<tr><td>Start</td><td>%s</td></tr>", stats.StartTime.Format(time.RFC3339)))
	body.WriteString(fmt.Sprintf("<tr><td>End</td><td>%s</td></tr>", stats.EndTime.Format(time.RFC3339)))
	body.WriteString(fmt.Sprintf("<tr><td>Duration</td><td>%v</td></tr>", elapsed))
	body.WriteString(fmt.Sprintf("<tr><td>CPU Count</td><td>%d</td></tr>", runtime.NumCPU()))
	body.WriteString(fmt.Sprintf("<tr><td>GOMAXPROCS</td><td>%d</td></tr>", runtime.GOMAXPROCS(0)))
	if len(stats.Errors) > 0 {
		body.WriteString("<tr><td>Errors</td><td><ul>")
		for _, e := range stats.Errors {
			body.WriteString("<li>")
			body.WriteString(htmlEscape(e))
			body.WriteString("</li>")
		}
		body.WriteString("</ul></td></tr>")
	}
	body.WriteString("</table>")
	body.WriteString(fmt.Sprintf("<p><small>%s</small></p>", htmlEscape(toolName)))
	body.WriteString("</body></html>")

	addr := fmt.Sprintf("%s:%d", config.EmailNotification.SMTPHost, config.EmailNotification.SMTPPort)
	auth := smtp.PlainAuth("", config.EmailNotification.Username, config.EmailNotification.Password, config.EmailNotification.SMTPHost)

	// Build MIME message for HTML
	msg := strings.Builder{}
	msg.WriteString(fmt.Sprintf("From: %s\r\n", config.EmailNotification.From))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", config.EmailNotification.To))
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: text/html; charset=utf-8\r\n\r\n")
	msg.WriteString(body.String())

	if err := smtp.SendMail(addr, auth, config.EmailNotification.From, []string{config.EmailNotification.To}, []byte(msg.String())); err != nil {
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			return fmt.Errorf("smtp timeout: %v", err)
		}
		return err
	}
	return nil
}

// htmlEscape is a minimal escaper for text in HTML context
func htmlEscape(s string) string {
	r := strings.ReplaceAll(s, "&", "&amp;")
	r = strings.ReplaceAll(r, "<", "&lt;")
	r = strings.ReplaceAll(r, ">", "&gt;")
	return r
}

// writeRunReport saves a detailed text report next to the executable
func writeRunReport() error {
	exeDir, err := os.Getwd()
	if err != nil {
		return err
	}
	name := fmt.Sprintf("compression_report_%s.txt", time.Now().Format("YYYYMMDD_HHmmss"))
	// fix Go time format
	name = fmt.Sprintf("compression_report_%s.txt", time.Now().Format("20060102_150405"))
	path := filepath.Join(exeDir, name)

	elapsed := stats.EndTime.Sub(stats.StartTime)
	throughputMBs := 0.0
	if elapsed > 0 && stats.TotalSizeBefore > 0 {
		throughputMBs = (float64(stats.TotalSizeBefore) / (1024 * 1024)) / elapsed.Seconds()
	}

	b := &strings.Builder{}
	b.WriteString(toolName + "\n")
	b.WriteString(strings.Repeat("=", 60) + "\n")
	b.WriteString(fmt.Sprintf("Start: %s\n", stats.StartTime.Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("End:   %s\n", stats.EndTime.Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("Duration: %v\n", elapsed))
	b.WriteString(fmt.Sprintf("CPU Count: %d\n", runtime.NumCPU()))
	b.WriteString(fmt.Sprintf("GOMAXPROCS: %d\n", runtime.GOMAXPROCS(0)))
	b.WriteString(fmt.Sprintf("Groups (months): %d\n", stats.GroupCount))
	b.WriteString(fmt.Sprintf("Files processed: %d\n", stats.FilesProcessed))
	b.WriteString(fmt.Sprintf("Files compressed: %d\n", stats.FilesCompressed))
	b.WriteString(fmt.Sprintf("Total before: %.2f MB\n", float64(stats.TotalSizeBefore)/(1024*1024)))
	b.WriteString(fmt.Sprintf("Total after: %.2f MB\n", float64(stats.TotalSizeAfter)/(1024*1024)))
	if stats.TotalSizeBefore > 0 {
		reduction := float64(stats.TotalSizeBefore-stats.TotalSizeAfter) / float64(stats.TotalSizeBefore) * 100
		b.WriteString(fmt.Sprintf("Compression ratio: %.2f%%\n", reduction))
	}
	b.WriteString(fmt.Sprintf("Throughput: %.2f MB/s\n", throughputMBs))
	b.WriteString(fmt.Sprintf("Email status: %s\n", stats.EmailStatus))
	if len(stats.Errors) > 0 {
		b.WriteString("Errors:\n")
		for _, e := range stats.Errors {
			b.WriteString(" - ")
			b.WriteString(e)
			b.WriteString("\n")
		}
	}
	b.WriteString("\nOpen-source: Free to use. Do whatever you want with it.\n")
	b.WriteString("Maker: Nader Barakat (www.naderb.org)\n")
	return os.WriteFile(path, []byte(b.String()), 0644)
}

// deleteWithRetry attempts to remove a file multiple times
func deleteWithRetry(path string, attempts int, delay time.Duration) error {
	var lastErr error
	for i := 0; i < attempts; i++ {
		if err := os.Remove(path); err != nil {
			lastErr = err
			time.Sleep(delay)
			continue
		}
		return nil
	}
	return lastErr
}
