package utils

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// AtomicWriteFile writes data to a file atomically using a temporary file
// This prevents corruption if the process is interrupted during writing
func AtomicWriteFile(filePath string, data []byte, perm os.FileMode) error {
	// Ensure directory exists
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Create temporary file
	tempFile := filePath + ".tmp"
	file, err := os.Create(tempFile)
	if err != nil {
		return fmt.Errorf("failed to create temporary file %s: %w", tempFile, err)
	}

	// Write data
	written, writeErr := file.Write(data)
	if writeErr == nil && written != len(data) {
		writeErr = io.ErrShortWrite
	}

	// Close file
	closeErr := file.Close()

	// Check for write error
	if writeErr != nil {
		cause := fmt.Errorf("failed to write to temporary file %s: %w", tempFile, writeErr)
		return cleanupAtomicFile(tempFile, cause)
	}

	// Check for close error
	if closeErr != nil {
		cause := fmt.Errorf("failed to close temporary file %s: %w", tempFile, closeErr)
		return cleanupAtomicFile(tempFile, cause)
	}

	// Set file permissions
	if err := os.Chmod(tempFile, perm); err != nil {
		cause := fmt.Errorf("failed to set permissions on temporary file %s: %w", tempFile, err)
		return cleanupAtomicFile(tempFile, cause)
	}

	// Atomically move temporary file to final location
	if err := os.Rename(tempFile, filePath); err != nil {
		cause := fmt.Errorf("failed to rename temporary file %s to %s: %w", tempFile, filePath, err)
		return cleanupAtomicFile(tempFile, cause)
	}

	return nil
}

func cleanupAtomicFile(path string, cause error) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.Join(cause, fmt.Errorf("failed to remove temporary file %s: %w", path, err))
	}
	return cause
}

// SafeClose safely closes a file with error logging
func SafeClose(file *os.File, fileName string) {
	if file != nil {
		if err := file.Close(); err != nil {
			fmt.Printf("Warning: failed to close file %s: %v\n", fileName, err)
		}
	}
}

// EnsureDir ensures that a directory exists, creating it if necessary
func EnsureDir(dir string, perm os.FileMode) error {
	if err := os.MkdirAll(dir, perm); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}
	return nil
}
