package security

import (
	"errors"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

var (
	ErrPathTraversal = errors.New("path traversal attempt detected")
	ErrInvalidPath   = errors.New("invalid path characters")
	ErrPathTooLong   = errors.New("path exceeds maximum length")
	validPathSegment = regexp.MustCompile(`^[a-z0-9_-]+$`)
	validFileSegment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

func normalizePath(input string) string {
	if input == "" {
		return ""
	}
	return strings.Trim(path.Clean(input), "/")
}

func validatePathSegment(segment string) error {
	if segment == "" || !validPathSegment.MatchString(segment) {
		return ErrInvalidPath
	}
	if len(segment) > 64 {
		return ErrPathTooLong
	}
	if segment == "." || segment == ".." {
		return ErrPathTraversal
	}
	return nil
}

func validatePath(value string) error {
	if len(value) > 2048 {
		return ErrPathTooLong
	}
	if strings.Contains(value, "..") {
		return ErrPathTraversal
	}
	for _, segment := range strings.Split(strings.Trim(value, "/"), "/") {
		if segment == "" {
			continue
		}
		if err := validatePathSegment(segment); err != nil {
			return err
		}
	}
	return nil
}

// SafePath normalizes a URL prefix or configured identifier.
func SafePath(input string) (string, error) {
	if err := validatePath(input); err != nil {
		return "", err
	}
	return normalizePath(input), nil
}

// SafeDirPath validates a filesystem directory override without changing its rootedness.
func SafeDirPath(input string) (string, error) {
	if input == "" {
		return "", ErrInvalidPath
	}
	for _, char := range input {
		if unicode.IsControl(char) {
			return "", ErrInvalidPath
		}
	}
	for _, segment := range strings.Split(filepath.ToSlash(input), "/") {
		if segment == ".." {
			return "", ErrPathTraversal
		}
	}
	return filepath.Clean(input), nil
}

// SafeResourcePath validates a relative resource path while preserving case and dots.
func SafeResourcePath(input string) (string, error) {
	if len(input) > 2048 || strings.Contains(input, "\\") {
		return "", ErrPathTraversal
	}
	segments := strings.Split(strings.Trim(input, "/"), "/")
	kept := make([]string, 0, len(segments))
	for _, segment := range segments {
		if segment == "" {
			continue
		}
		if len(segment) > 255 {
			return "", ErrPathTooLong
		}
		if !validFileSegment.MatchString(segment) {
			return "", ErrInvalidPath
		}
		kept = append(kept, segment)
	}
	if len(kept) == 0 {
		return "", ErrInvalidPath
	}
	return strings.Join(kept, "/"), nil
}

// SafeFilePath ensures a validated resource remains inside its base directory.
func SafeFilePath(baseDir, userPath string) (string, error) {
	safe, err := SafeResourcePath(userPath)
	if err != nil {
		return "", err
	}
	base, err := filepath.Abs(baseDir)
	if err != nil {
		return "", err
	}
	full, err := filepath.Abs(filepath.Join(base, filepath.FromSlash(safe)))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(base, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", ErrPathTraversal
	}
	return full, nil
}
