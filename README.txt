IIS Log compressor by Nader Barakat - www.naderb.org tools
===========================================================
Open-source: Free to use. Do whatever you want with it.

What this tool does
- Compresses IIS log files into monthly ZIP archives (one ZIP per month)
- Verifies that files are inside the ZIP before any deletion
- Can optionally delete original logs after successful verification
- Writes a run report file after each execution
- Can send an HTML email summary

Files
- iis-log-compressor.exe  -> the application
- config.json             -> configuration (same folder as the EXE)
- compression_report_*.txt -> generated after each run

Quick start
1) Place iis-log-compressor.exe and config.json in the same folder
2) Edit config.json:
   - source_folder: IIS logs path (e.g., C:\inetpub\logs\LogFiles\W3SVC1)
   - dest_folder: where monthly ZIPs will be created
   - log_age_days: only compress logs older than this many days
   - retention_days: delete compressed files older than this many days (if cleanup_old_logs = true)
   - cleanup_old_logs: true/false to enable retention cleanup in dest_folder
   - delete_original_after_compress: true/false to delete originals after verifying ZIP contents (default false)
   - dest_file_name_pattern: suggested for monthly -> "iis_logs_%Y_%m"
   - compression_type: "zip" (monthly mode requires zip)
   - max_cpus: 0 to use all CPUs, or set a number
   - email_notification: SMTP settings; set enabled=true to send email
3) Run: iis-log-compressor.exe

Email
- Subject: "Success - IIS backup <hostname>" or "Failed - IIS backup <hostname>"
- HTML body includes summary table; footer shows the tool name
- If email fails, the error is recorded in the run report

Scheduling (Windows Task Scheduler)
- Action: Start a program -> iis-log-compressor.exe
- Start in: folder containing the EXE and config.json
- Run with highest privileges if needed
- Suggested trigger: daily outside peak hours

Safety & verification
- The app verifies that each file exists in the monthly ZIP with matching uncompressed size before deleting originals
- By default, delete_original_after_compress = false
- Test on a copy of your logs first

Troubleshooting
- JSON parse error: Ensure Windows paths in config.json use double backslashes (\\) or forward slashes (/)
- File in use: IIS or AV may lock files; the app retries deletion if enabled
- No ZIP created: Check permissions on dest_folder and free disk space
- Email failed: Verify SMTP host/port/credentials and allow app passwords if required

Performance information in report
- Start/End time and total duration
- CPU count and GOMAXPROCS used
- Number of month groups, files processed
- Total size before/after and compression ratio
- Throughput estimate (MB/s)
- Email status and any errors

Author
- Maker: Nader Barakat
- Website: https://www.naderb.org
- Tool name: IIS Log compressor by Nader Barakat . www.naderb.org tools

License
- Open-source and free. No payment required. You may use, modify, and distribute this tool without restriction.

New features in this build
- Choose archive scope: monthly (default) or daily -> set "archive_scope"
- Control current period compression: set "compress_current_month" (monthly) or today excluded by default in daily mode
- KeepLastNArchives cleanup: set "keep_last_n_archives" to keep only the most recent N archives (overrides retention_days). Set 0 to disable
- Config flag "delete_original_after_compress" (default false)

Config quick reference additions
- archive_scope: "monthly" or "daily"
- compress_current_month: true/false (applies to monthly scope)
- keep_last_n_archives: integer (0 disables)
