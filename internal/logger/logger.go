package logger

import (
	"fmt"
	"log"
	"os"
	"time"
)

// LogLevel represents the level of logging
type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
)

var (
	logger    *log.Logger
	logLevel  LogLevel
	logFile   *os.File
)

// Init initializes the logger with the specified log level and output file
func Init(level LogLevel, filePath string) error {
	logLevel = level
	
	var err error
	if filePath != "" {
		// Open or create the log file
		logFile, err = os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			return fmt.Errorf("failed to open log file: %v", err)
		}
		logger = log.New(logFile, "", 0)
	} else {
		// Log to stdout if no file path is provided
		logger = log.New(os.Stdout, "", 0)
	}
	
	return nil
}

// Close closes the log file if it was opened
func Close() {
	if logFile != nil {
		logFile.Close()
	}
}

// Debug logs a debug message
func Debug(format string, v ...interface{}) {
	if logLevel <= DEBUG {
		logWithLevel("DEBUG", format, v...)
	}
}

// Info logs an info message
func Info(format string, v ...interface{}) {
	if logLevel <= INFO {
		logWithLevel("INFO", format, v...)
	}
}

// Warn logs a warning message
func Warn(format string, v ...interface{}) {
	if logLevel <= WARN {
		logWithLevel("WARN", format, v...)
	}
}

// Error logs an error message
func Error(format string, v ...interface{}) {
	if logLevel <= ERROR {
		logWithLevel("ERROR", format, v...)
	}
}

// logWithLevel logs a message with the specified level prefix
func logWithLevel(level, format string, v ...interface{}) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	message := fmt.Sprintf(format, v...)
	logger.Printf("[%s] [%s] %s\n", timestamp, level, message)
}