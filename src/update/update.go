package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var releasesURL = "https://api.github.com/repos/apimgr/mail/releases"

type Release struct {
	TagName     string    `json:"tag_name"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	Assets      []Asset   `json:"assets"`
}

type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

var client = &http.Client{Timeout: 30 * time.Second}

func NormalizeBranch(branch string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(branch)) {
	case "", "stable":
		return "stable", nil
	case "beta":
		return "beta", nil
	case "devel", "daily", "rolling", "dev", "development":
		return "devel", nil
	default:
		return "", fmt.Errorf("unsupported update branch %q (expected stable, beta, or devel)", branch)
	}
}

func SaveBranch(path, branch string) error {
	branch, err := NormalizeBranch(branch)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	node := &yaml.Node{}
	if len(data) == 0 {
		node = &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	} else if err := yaml.Unmarshal(data, node); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	if len(node.Content) == 0 || node.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("%s must contain a YAML mapping", path)
	}
	root := node.Content[0]
	for _, key := range []string{"server", "update"} {
		child := yamlValue(root, key)
		if child == nil {
			child = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, child)
		} else if child.Kind != yaml.MappingNode {
			return fmt.Errorf("%s.%s must be a mapping", path, key)
		}
		root = child
	}
	value := yamlValue(root, "branch")
	if value == nil {
		root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "branch"}, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: branch})
	} else {
		value.Kind, value.Tag, value.Value, value.Content = yaml.ScalarNode, "!!str", branch, nil
	}
	encoded, err := yaml.Marshal(node)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm() & 0o600
		if mode == 0 {
			mode = 0o600
		}
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".server.yml-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer func() { _ = os.Remove(name) }()
	if err := temp.Chmod(mode); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(encoded); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func ReadBranch(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "stable"
	}
	var document struct {
		Server struct {
			Update struct {
				Branch string `yaml:"branch"`
			} `yaml:"update"`
		} `yaml:"server"`
	}
	if yaml.Unmarshal(data, &document) != nil {
		return "stable"
	}
	branch, err := NormalizeBranch(document.Server.Update.Branch)
	if err != nil {
		return "stable"
	}
	return branch
}

func yamlValue(mapping *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

func Check(ctx context.Context, currentVersion, branch string, epoch int64) (*Release, error) {
	return CheckWithDefer(ctx, currentVersion, branch, epoch, 0, time.Now().UTC())
}

// CheckWithDefer selects the newest eligible release for scheduled checks.
func CheckWithDefer(ctx context.Context, currentVersion, branch string, epoch int64, deferDays int, now time.Time) (*Release, error) {
	branch, err := NormalizeBranch(branch)
	if err != nil {
		return nil, err
	}
	if deferDays < 0 || deferDays > 365 {
		return nil, fmt.Errorf("defer days must be between 0 and 365")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	url := releasesURL
	if branch == "stable" && deferDays == 0 {
		url += "/latest"
	} else {
		url += "?per_page=100"
	}
	var releases []Release
	if branch == "stable" && deferDays == 0 {
		release, found, err := fetchStableRelease(ctx, url)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, nil
		}
		releases = []Release{release}
	} else {
		releases, err = fetchReleasePages(ctx, url, currentVersion)
		if err != nil {
			return nil, err
		}
	}
	return selectReleaseWithDefer(releases, currentVersion, branch, epoch, deferDays, now), nil
}

func fetchStableRelease(ctx context.Context, releaseURL string) (Release, bool, error) {
	body, status, _, err := getReleasePage(ctx, releaseURL)
	if err != nil {
		return Release{}, false, err
	}
	if status == http.StatusNotFound {
		return Release{}, false, nil
	}
	if status != http.StatusOK {
		return Release{}, false, fmt.Errorf("GitHub Releases API returned HTTP %d", status)
	}
	var release Release
	if err := json.Unmarshal(body, &release); err != nil {
		return Release{}, false, fmt.Errorf("decode latest stable release: %w", err)
	}
	return release, true, nil
}

func fetchReleasePages(ctx context.Context, nextURL, currentVersion string) ([]Release, error) {
	const maxReleasePages = 100
	var releases []Release
	for page := 0; nextURL != "" && page < maxReleasePages; page++ {
		body, status, linkHeader, err := getReleasePage(ctx, nextURL)
		if err != nil {
			return nil, err
		}
		if status == http.StatusNotFound {
			if page == 0 {
				return nil, nil
			}
			break
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("GitHub Releases API returned HTTP %d", status)
		}
		var batch []Release
		if err := json.Unmarshal(body, &batch); err != nil {
			return nil, fmt.Errorf("decode GitHub release page: %w", err)
		}
		releases = append(releases, batch...)
		if currentVersion != "" && releasePageHasTag(batch, currentVersion) {
			nextURL = ""
			break
		}
		nextURL, err = nextReleasePageURL(linkHeader, nextURL)
		if err != nil {
			return nil, err
		}
	}
	if nextURL != "" {
		return nil, fmt.Errorf("GitHub Releases API exceeded %d pages", maxReleasePages)
	}
	return releases, nil
}

func getReleasePage(ctx context.Context, requestURL string) ([]byte, int, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, 0, "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, resp.StatusCode, "", fmt.Errorf("read GitHub Releases response: %w", err)
	}
	return body, resp.StatusCode, resp.Header.Get("Link"), nil
}

func nextReleasePageURL(linkHeader, currentURL string) (string, error) {
	for _, part := range strings.Split(linkHeader, ",") {
		sections := strings.Split(part, ";")
		if len(sections) < 2 || !strings.Contains(sections[1], `rel="next"`) {
			continue
		}
		link := strings.TrimSpace(sections[0])
		if len(link) < 3 || link[0] != '<' || link[len(link)-1] != '>' {
			return "", fmt.Errorf("GitHub Releases API returned an invalid pagination link")
		}
		current, err := url.Parse(currentURL)
		if err != nil {
			return "", fmt.Errorf("parse current release URL: %w", err)
		}
		next, err := current.Parse(link[1 : len(link)-1])
		if err != nil || next.Scheme != "https" || next.Host != current.Host || next.User != nil {
			return "", fmt.Errorf("GitHub Releases API returned an unsafe pagination link")
		}
		return next.String(), nil
	}
	return "", nil
}

func releasePageHasTag(releases []Release, tag string) bool {
	for _, release := range releases {
		if sameTag(release.TagName, tag) {
			return true
		}
	}
	return false
}

func selectRelease(releases []Release, currentVersion, branch string, epoch int64) *Release {
	return selectReleaseWithDefer(releases, currentVersion, branch, epoch, 0, time.Now().UTC())
}

func selectReleaseWithDefer(releases []Release, currentVersion, branch string, epoch int64, deferDays int, now time.Time) *Release {
	currentIndex := -1
	for i := range releases {
		if sameTag(releases[i].TagName, currentVersion) {
			currentIndex = i
			break
		}
	}
	for i := range releases {
		r := &releases[i]
		if !matches(*r, branch) {
			continue
		}
		if branch != "devel" && deferDays > 0 && (r.PublishedAt.IsZero() || now.Before(r.PublishedAt.Add(time.Duration(deferDays)*24*time.Hour))) {
			continue
		}
		if r.TagName == "devel" {
			if r.PublishedAt.Unix() > epoch {
				return r
			}
			continue
		}
		if currentIndex >= 0 && i < currentIndex {
			return r
		}
		if currentIndex >= 0 {
			return nil
		}
		if sameVersionFormat(r.TagName, currentVersion) {
			if compareVersions(r.TagName, currentVersion) > 0 {
				return r
			}
			return nil
		}
		if epoch > 0 && r.PublishedAt.Unix() > epoch {
			return r
		}
		return nil
	}
	return nil
}

func matches(r Release, branch string) bool {
	if !r.Prerelease {
		return true
	}
	beta := strings.HasSuffix(strings.ToLower(r.TagName), "-beta")
	return branch == "beta" && beta || branch == "devel" && (beta || r.TagName == "devel")
}

func compareVersions(left, right string) int {
	leftParts, rightParts := versionParts(left), versionParts(right)
	for i := 0; i < len(leftParts) || i < len(rightParts); i++ {
		var a, b int64
		if i < len(leftParts) {
			a = leftParts[i]
		}
		if i < len(rightParts) {
			b = rightParts[i]
		}
		if a < b {
			return -1
		}
		if a > b {
			return 1
		}
	}
	return 0
}

func sameTag(left, right string) bool {
	return strings.TrimPrefix(strings.TrimSpace(left), "v") == strings.TrimPrefix(strings.TrimSpace(right), "v")
}

func sameVersionFormat(left, right string) bool {
	return strings.HasSuffix(strings.ToLower(left), "-beta") == strings.HasSuffix(strings.ToLower(right), "-beta")
}

func versionParts(value string) []int64 {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	var parts []int64
	for _, part := range strings.FieldsFunc(value, func(r rune) bool { return r < '0' || r > '9' }) {
		number, _ := strconv.ParseInt(part, 10, 64)
		parts = append(parts, number)
	}
	return parts
}

func AssetName(goos, goarch string) string {
	name := "mail-" + goos + "-" + goarch
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

// ReleaseID distinguishes successive builds of the rolling devel tag.
func ReleaseID(release *Release) string {
	if release == nil {
		return ""
	}
	if release.TagName == "devel" && !release.PublishedAt.IsZero() {
		return fmt.Sprintf("devel@%d", release.PublishedAt.UTC().Unix())
	}
	return strings.TrimSpace(release.TagName)
}

func Install(ctx context.Context, release *Release) error {
	assetName := AssetName(runtime.GOOS, runtime.GOARCH)
	var binaryURL, checksumURL string
	for _, asset := range release.Assets {
		switch asset.Name {
		case assetName:
			binaryURL = asset.URL
		case "sha256.txt":
			checksumURL = asset.URL
		}
	}
	if binaryURL == "" || checksumURL == "" {
		return fmt.Errorf("release must include %s and sha256.txt", assetName)
	}
	base := filepath.Join(os.TempDir(), "apimgr")
	if err := os.MkdirAll(base, 0o700); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(base, "mail-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	binPath := filepath.Join(dir, assetName)
	if err := download(ctx, binaryURL, binPath); err != nil {
		return err
	}
	checksums, err := fetch(ctx, checksumURL)
	if err != nil {
		return fmt.Errorf("fetch release checksum: %w", err)
	}
	want, err := parseChecksum(string(checksums), assetName)
	if err != nil {
		return err
	}
	if err := verify(binPath, want); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(binPath, 0o755); err != nil {
			return err
		}
	}
	current, err := os.Executable()
	if err != nil {
		return err
	}
	current, err = filepath.EvalSymlinks(current)
	if err != nil {
		return err
	}
	if err := replaceBinary(current, binPath); err != nil {
		return err
	}
	return restartSelf()
}

// StageRelease downloads and verifies a release into the persistent update
// directory. The returned path is the staged binary; its sidecar metadata is
// intentionally treated as untrusted by ApplyStaged.
func StageRelease(ctx context.Context, release *Release, updateDir string) (string, error) {
	if release == nil || strings.TrimSpace(release.TagName) == "" {
		return "", fmt.Errorf("release tag is required")
	}
	assetName := AssetName(runtime.GOOS, runtime.GOARCH)
	binaryURL, checksumURL, err := releaseURLs(release, assetName)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(updateDir, 0o700); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(updateDir), ".mail-update-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	tmpBinary := filepath.Join(tmp, "mail.new")
	if err := download(ctx, binaryURL, tmpBinary); err != nil {
		return "", err
	}
	checksums, err := fetch(ctx, checksumURL)
	if err != nil {
		return "", fmt.Errorf("fetch release checksum: %w", err)
	}
	digest, err := parseChecksum(string(checksums), assetName)
	if err != nil {
		return "", err
	}
	if err := verify(tmpBinary, digest); err != nil {
		return "", err
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(tmpBinary, 0o755); err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(filepath.Join(tmp, "sha256"), []byte(digest+"\n"), 0o600); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(tmp, "version"), []byte(release.TagName+"\n"), 0o600); err != nil {
		return "", err
	}
	if !release.PublishedAt.IsZero() {
		if err := os.WriteFile(filepath.Join(tmp, "published_at"), []byte(release.PublishedAt.UTC().Format(time.RFC3339Nano)+"\n"), 0o600); err != nil {
			return "", err
		}
	}
	if err := os.RemoveAll(updateDir); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, updateDir); err != nil {
		return "", err
	}
	return filepath.Join(updateDir, "mail.new"), nil
}

// ApplyStaged performs the privileged startup phase. It re-fetches the exact
// release checksum over verified HTTPS and never trusts staged sidecar data.
func ApplyStaged(ctx context.Context, updateDir, currentPath, runningVersion string, runningEpoch int64) (string, error) {
	versionBytes, err := os.ReadFile(filepath.Join(updateDir, "version"))
	if err != nil {
		return "", err
	}
	version := strings.TrimSpace(string(versionBytes))
	if version == "" || (version != "devel" && (sameTag(version, runningVersion) || compareVersions(version, runningVersion) <= 0)) {
		return "", fmt.Errorf("staged release %q is not newer than running version %q", version, runningVersion)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	release, err := fetchReleaseByTag(ctx, version)
	if err != nil {
		return "", err
	}
	if version == "devel" && (runningEpoch <= 0 || release.PublishedAt.Unix() <= runningEpoch) {
		return "", fmt.Errorf("staged devel release is not newer than the running build epoch")
	}
	assetName := AssetName(runtime.GOOS, runtime.GOARCH)
	_, checksumURL, err := releaseURLs(&release, assetName)
	if err != nil {
		return "", err
	}
	checksums, err := fetch(ctx, checksumURL)
	if err != nil {
		return "", fmt.Errorf("re-fetch official release checksum: %w", err)
	}
	digest, err := parseChecksum(string(checksums), assetName)
	if err != nil {
		return "", err
	}
	staged := filepath.Join(updateDir, "mail.new")
	if err := verify(staged, digest); err != nil {
		return "", err
	}
	currentDir := filepath.Dir(currentPath)
	install, err := os.CreateTemp(currentDir, ".mail-install-*")
	if err != nil {
		return "", err
	}
	installPath := install.Name()
	defer func() { _ = os.Remove(installPath) }()
	if err := install.Chmod(0o700); err != nil {
		_ = install.Close()
		return "", err
	}
	source, err := os.Open(staged)
	if err != nil {
		_ = install.Close()
		return "", err
	}
	_, copyErr := io.Copy(install, source)
	closeSourceErr := source.Close()
	if copyErr != nil {
		_ = install.Close()
		return "", copyErr
	}
	if closeSourceErr != nil {
		_ = install.Close()
		return "", closeSourceErr
	}
	if err := install.Sync(); err != nil {
		_ = install.Close()
		return "", err
	}
	if err := install.Close(); err != nil {
		return "", err
	}
	if err := replaceBinary(currentPath, installPath); err != nil {
		return "", err
	}
	if err := os.RemoveAll(updateDir); err != nil {
		return "", err
	}
	return version, nil
}

func fetchReleaseByTag(ctx context.Context, tag string) (Release, error) {
	base, err := url.Parse(releasesURL)
	if err != nil {
		return Release{}, err
	}
	base.Path = strings.TrimSuffix(base.Path, "/") + "/tags/" + url.PathEscape(tag)
	body, status, _, err := getReleasePage(ctx, base.String())
	if err != nil {
		return Release{}, err
	}
	if status != http.StatusOK {
		return Release{}, fmt.Errorf("GitHub release tag lookup returned HTTP %d", status)
	}
	var release Release
	if err := json.Unmarshal(body, &release); err != nil {
		return Release{}, fmt.Errorf("decode GitHub release: %w", err)
	}
	if !sameTag(release.TagName, tag) {
		return Release{}, fmt.Errorf("GitHub returned a different release tag")
	}
	return release, nil
}

func releaseURLs(release *Release, assetName string) (string, string, error) {
	var binaryURL, checksumURL string
	for _, asset := range release.Assets {
		if asset.Name == assetName {
			binaryURL = asset.URL
		}
		if asset.Name == "sha256.txt" {
			checksumURL = asset.URL
		}
	}
	if binaryURL == "" || checksumURL == "" {
		return "", "", fmt.Errorf("release must include %s and sha256.txt", assetName)
	}
	for _, raw := range []string{binaryURL, checksumURL} {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil {
			return "", "", fmt.Errorf("release contains an unsafe asset URL")
		}
	}
	return binaryURL, checksumURL, nil
}

func download(ctx context.Context, url, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download update: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned %s", resp.Status)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, io.LimitReader(resp.Body, 512<<20)); err != nil {
		_ = file.Close()
		return fmt.Errorf("download update: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download returned %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func parseChecksum(contents, assetName string) (string, error) {
	for _, line := range strings.Split(contents, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == assetName {
			if len(fields[0]) != sha256.Size*2 {
				break
			}
			if _, err := hex.DecodeString(fields[0]); err != nil {
				break
			}
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("sha256.txt has no valid checksum for %s", assetName)
}

func verify(path, expected string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if actual != strings.ToLower(expected) {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expected, actual)
	}
	return nil
}
