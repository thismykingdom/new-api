package common

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const GeneratedImageRoutePrefix = "/generated-images/"

const (
	defaultGeneratedImageMaxAge          = 72 * time.Hour
	defaultGeneratedImageCleanupInterval = time.Hour
)

var generatedImageCleanupOnce sync.Once

func GetGeneratedImageDir() string {
	if dir := strings.TrimSpace(os.Getenv("GENERATED_IMAGE_DIR")); dir != "" {
		return dir
	}
	cachePath := strings.TrimSpace(GetDiskCachePath())
	if cachePath != "" {
		return filepath.Join(cachePath, "generated-images")
	}
	return filepath.Join(".", "generated-images")
}

func GetGeneratedImageMaxAge() time.Duration {
	if duration := parseGeneratedImageDuration("GENERATED_IMAGE_MAX_AGE"); duration >= 0 {
		return duration
	}
	if hours := parseGeneratedImageHours("GENERATED_IMAGE_MAX_AGE_HOURS"); hours >= 0 {
		return hours
	}
	return defaultGeneratedImageMaxAge
}

func GetGeneratedImageCleanupInterval() time.Duration {
	if duration := parseGeneratedImageDuration("GENERATED_IMAGE_CLEANUP_INTERVAL"); duration > 0 {
		return duration
	}
	if minutes := parseGeneratedImageMinutes("GENERATED_IMAGE_CLEANUP_INTERVAL_MINUTES"); minutes > 0 {
		return minutes
	}
	return defaultGeneratedImageCleanupInterval
}

func CleanupOldGeneratedImages(maxAge time.Duration) (int, int64, error) {
	if maxAge <= 0 {
		return 0, 0, nil
	}
	dir := GetGeneratedImageDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil
		}
		return 0, 0, err
	}

	cutoff := time.Now().Add(-maxAge)
	var removedCount int
	var removedBytes int64
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err == nil {
			removedCount++
			removedBytes += info.Size()
		}
	}
	return removedCount, removedBytes, nil
}

func StartGeneratedImageCleanupTask() {
	generatedImageCleanupOnce.Do(func() {
		if !IsMasterNode {
			return
		}
		maxAge := GetGeneratedImageMaxAge()
		if maxAge <= 0 {
			SysLog("generated image cleanup disabled")
			return
		}
		interval := GetGeneratedImageCleanupInterval()
		go func() {
			SysLog("generated image cleanup task started")
			runGeneratedImageCleanup(maxAge)
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for range ticker.C {
				runGeneratedImageCleanup(maxAge)
			}
		}()
	})
}

func runGeneratedImageCleanup(maxAge time.Duration) {
	removedCount, removedBytes, err := CleanupOldGeneratedImages(maxAge)
	if err != nil {
		SysError("generated image cleanup failed: " + err.Error())
		return
	}
	if removedCount > 0 {
		SysLog("generated image cleanup removed files: count=" + strconv.Itoa(removedCount) + " bytes=" + strconv.FormatInt(removedBytes, 10))
	}
}

func parseGeneratedImageDuration(name string) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return -1
	}
	if value == "0" {
		return 0
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return -1
	}
	return duration
}

func parseGeneratedImageHours(name string) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return -1
	}
	hours, err := strconv.Atoi(value)
	if err != nil {
		return -1
	}
	return time.Duration(hours) * time.Hour
}

func parseGeneratedImageMinutes(name string) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return -1
	}
	minutes, err := strconv.Atoi(value)
	if err != nil {
		return -1
	}
	return time.Duration(minutes) * time.Minute
}
