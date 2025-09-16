# IIS Log Compression Tool

A high-performance console application written in Go for compressing IIS logs with multiple compression algorithms and parallel processing.

## Features

- **Multiple Compression Algorithms**: ZIP, GZIP, LZ4, ZSTD
- **Parallel Processing**: Utilizes all available CPUs or configurable CPU count
- **Configurable Retention**: Automatic cleanup of old compressed logs
- **Email Notifications**: Optional email reports after compression
- **Flexible Configuration**: JSON-based configuration file
- **Windows Executable**: Single .exe file for easy deployment

## Compression Options

The tool supports the following compression algorithms:

1. **ZIP** - Standard ZIP compression (default)
2. **GZIP** - Fast compression with good ratio
3. **LZ4** - Very fast compression, lower ratio
4. **ZSTD** - Modern algorithm with excellent speed/ratio balance

## Configuration

Edit `config.json` to configure the application:

```json
{
  "source_folder": "C:\\inetpub\\logs\\LogFiles",
  "dest_folder": "C:\\Logs\\Compressed",
  "log_age_days": 7,
  "retention_days": 30,
  "cleanup_old_logs": true,
  "dest_file_name_pattern": "iis_logs_%Y%m%d_%H%M%S",
  "compression_type": "zip",
  "max_cpus": 0,
  "email_notification": {
    "enabled": false,
    "smtp_host": "smtp.gmail.com",
    "smtp_port": 587,
    "username": "your-email@gmail.com",
    "password": "your-app-password",
    "from": "your-email@gmail.com",
    "to": "admin@company.com",
    "subject": "IIS Log Compression Report"
  }
}
```

### Configuration Options

- **source_folder**: Path to IIS logs directory
- **dest_folder**: Where to store compressed logs
- **log_age_days**: Minimum age of logs to compress (in days)
- **retention_days**: How long to keep compressed logs
- **cleanup_old_logs**: Whether to delete old compressed logs
- **dest_file_name_pattern**: Pattern for compressed file names
  - `%Y` - 4-digit year
  - `%m` - 2-digit month
  - `%d` - 2-digit day
  - `%H` - 2-digit hour
  - `%M` - 2-digit minute
  - `%S` - 2-digit second
  - `%y` - 2-digit year
  - `%j` - day of year
- **compression_type**: One of: zip, gzip, lz4, zstd
- **max_cpus**: Maximum CPUs to use (0 = all available)
- **email_notification**: Email settings for notifications

## Building

### Prerequisites
- Go 1.21 or later
- Windows OS

### Build Steps
1. Open Command Prompt in the project directory
2. Run: `build.bat`
3. The executable `iis-log-compressor.exe` will be created

### Manual Build
```bash
go mod tidy
go build -o iis-log-compressor.exe main.go
```

## Usage

1. Place `iis-log-compressor.exe` and `config.json` in the same directory
2. Edit `config.json` with your settings
3. Run: `iis-log-compressor.exe`

## Performance

- **Parallel Processing**: Uses all available CPU cores by default
- **Memory Efficient**: Processes files one at a time to minimize memory usage
- **Fast Compression**: LZ4 and ZSTD provide excellent speed
- **High Compression**: ZIP and GZIP provide better compression ratios

## Compression Comparison

| Algorithm | Speed | Ratio | Use Case |
|-----------|-------|-------|----------|
| ZIP | Medium | High | General purpose |
| GZIP | Fast | High | Balanced performance |
| LZ4 | Very Fast | Medium | Speed critical |
| ZSTD | Fast | Very High | Modern applications |

## Error Handling

- Logs all errors to console
- Continues processing other files if one fails
- Provides detailed summary at the end
- Optional email notifications for errors

## Security

- No hardcoded credentials
- All sensitive data in configuration file
- Safe file operations with proper error handling

## License

This project is provided as-is for educational and commercial use.

## Download

just downlaod the following files to any windows machine to use the application: 
- iis-log-compressor.exe 
- README.txt
- config.json

Note: please read the "README.txt" file for more information. 

