package backup

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
	// "github.com/pelletier/go-toml/v2"
	// "github.com/vNodesV/vApp/modules/vProx/internal/log" // Uncomment if needed
	// "github.com/vNodesV/vApp/modules/vProx/internal/geo" // Uncomment if needed
)

type ConfigSet struct {
	cfg         string `toml:"cfgFile"`
	appVersion  string `toml:"appVersion"`
	goVersion   string `toml:"goVersion"`
	cfgLocation string `toml:"cfgLocation"`
}

type BackupFileManager struct {
	Destination string `toml:"destination"`
	LogFile     string `toml:"logFile"`
	LogLocation string `toml:"logLocation"`
	logDst      string `toml:"logDst"`
	Interval    int    `toml:"interval"`
	Timedate    string `toml:"timedate"`
	tarFile     string `toml:"tarFile"`
}

type BackupCompressor struct {
	Extension string `toml:"extension"`
	Level     string `toml:"level"`
}

type Backup struct {
	cfgFile     string            `toml:"HouseKeeping"`
	FileManager BackupFileManager `toml:"fileManager"`
	Compressor  BackupCompressor  `toml:"compressor"`
}

func (b *Backup) err() error {
	return errors.New("backup error")
}

// func (b *Backup) loadConfig() error {
// 	// Open/load toml config file.
// 	b = &Backup{}
// 	// Open/load toml config file.

// 	file, err := os.Open(b.cfgFile)
// 	if err != nil {
// 		return fmt.Errorf("failed to open config file: %w", b.err())
// 	}
// 	defer file.Close()

// 	if b == nil {

// 	}

// 	err = toml.NewDecoder(file).Decode(b)
// 	if err != nil {
// 		return fmt.Errorf("failed to decode config file: %w", err)
// 	}
// 	return nil
// }

func (b *Backup) Start() {
	//run performBackup every Interval
	ticker := time.NewTicker(time.Duration(b.FileManager.Interval) * time.Second)
	fmt.Printf("Starting backup every %d seconds (every %s hour)\n", b.FileManager.Interval, time.Duration(b.FileManager.Interval)*time.Second)
	defer ticker.Stop()
	if ticker == nil {
		return
	}

	for range ticker.C {
		if err := b.performBackup(*b); err != nil {
			fmt.Printf("Error performing backup: %v\n", err)
		}
	}
}

func (b *Backup) performBackup(Backup) error {

	// Create a timestamped backup directory
	err := os.MkdirAll(b.FileManager.logDst, 0755)

	if err != nil && b == nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}
	if b.FileManager.Timedate != "" {
		// Create a variable for the current timestamp for outfile filename.
		timestamp := time.Now().Format("20060102_150405")
		logEntry := fmt.Sprintf("%s/%s.%s.%s", b.FileManager.logDst, b.FileManager.LogFile, timestamp, b.Compressor.Extension)
		print(logEntry)
		// Create the tar file
		tarFile, err := os.Create(b.FileManager.tarFile)
		if err != nil {
			return fmt.Errorf("failed to create tar file: %w", err)
		}
		defer tarFile.Close()

		// Create a new tar writer
		tarWriter := tar.NewWriter(tarFile)
		defer tarWriter.Close()

		// Walk the source directory and add files to the tar
		err = filepath.Walk(b.FileManager.logDst, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			// Create a tar header for the file
			header, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}
			header.Name = filepath.ToSlash(path[len(b.FileManager.logDst+b.FileManager.LogFile):])
			if err := tarWriter.WriteHeader(header); err != nil {
				return err
			}
			// Copy the file contents to the tar
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			defer file.Close()
			if _, err := io.Copy(tarWriter, file); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("failed to walk source directory: %w", err)
		}
	}

	fmt.Printf("Backup completed successfully: %s\n", b.FileManager.logDst+"/"+b.FileManager.tarFile)
	return nil
}
func cpTar(b *Backup) error {
	// Copy the tar file to the destination
	srcFile, err := os.Open(b.FileManager.tarFile)
	if err != nil {
		return fmt.Errorf("failed to open tar file: %w", err)
	}
	defer srcFile.Close()

	dstFile, err := os.Create(filepath.Join(b.FileManager.Destination, filepath.Base(b.FileManager.tarFile)))
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("failed to copy tar file: %w", err)
	}

	return nil
}
