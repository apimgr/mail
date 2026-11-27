package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNormalizeBranch(t *testing.T) {
	for input, expected := range map[string]string{"stable": "stable", "beta": "beta", "daily": "devel", "rolling": "devel", "dev": "devel", "development": "devel", "devel": "devel"} {
		actual, err := NormalizeBranch(input)
		if err != nil || actual != expected {
			t.Errorf("NormalizeBranch(%q) = %q, %v; want %q", input, actual, err, expected)
		}
	}
	if _, err := NormalizeBranch("nightly"); err == nil {
		t.Fatal("unsupported branch was accepted")
	}
}

func TestCheckFetchesReleasePagesUntilCurrentVersion(t *testing.T) {
	oldURL, oldClient := releasesURL, client
	t.Cleanup(func() {
		releasesURL, client = oldURL, oldClient
	})
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			_ = json.NewEncoder(w).Encode([]Release{{TagName: "v1.2.3"}})
			return
		}
		next := server.URL + "/repos/apimgr/mail/releases?per_page=100&page=2"
		w.Header().Set("Link", "<"+next+`>; rel="next"`)
		_ = json.NewEncoder(w).Encode([]Release{{TagName: "202612011200-beta", Prerelease: true}, {TagName: "v1.2.4"}})
	}))
	defer server.Close()
	releasesURL = server.URL + "/repos/apimgr/mail/releases"
	client = server.Client()

	got, err := Check(context.Background(), "v1.2.3", "beta", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.TagName != "202612011200-beta" {
		t.Fatalf("Check() = %#v, want beta release from page one", got)
	}
}

func TestNextReleasePageURLRejectsCrossOriginLinks(t *testing.T) {
	_, err := nextReleasePageURL(`<https://attacker.example/releases?page=2>; rel="next"`, "https://api.github.com/repos/apimgr/mail/releases")
	if err == nil {
		t.Fatal("cross-origin pagination link was accepted")
	}
}

func TestMatchesUsesCumulativeChannels(t *testing.T) {
	stable := Release{TagName: "v1.2.3"}
	beta := Release{TagName: "202512051430-beta", Prerelease: true}
	devel := Release{TagName: "devel", Prerelease: true}
	if !matches(stable, "stable") || !matches(stable, "beta") || !matches(stable, "devel") {
		t.Fatal("stable release must match every channel")
	}
	if matches(beta, "stable") || !matches(beta, "beta") || !matches(beta, "devel") {
		t.Fatal("beta release channel matching is incorrect")
	}
	if matches(devel, "stable") || matches(devel, "beta") || !matches(devel, "devel") {
		t.Fatal("devel release channel matching is incorrect")
	}
}

func TestAssetName(t *testing.T) {
	if got := AssetName("linux", "amd64"); got != "mail-linux-amd64" {
		t.Fatalf("unexpected asset name %q", got)
	}
	if got := AssetName("windows", "arm64"); got != "mail-windows-arm64.exe" {
		t.Fatalf("unexpected Windows asset name %q", got)
	}
}

func TestReleaseIDDistinguishesRollingDevelBuilds(t *testing.T) {
	first := &Release{TagName: "devel", PublishedAt: time.Unix(100, 0)}
	second := &Release{TagName: "devel", PublishedAt: time.Unix(200, 0)}
	if ReleaseID(first) == ReleaseID(second) {
		t.Fatal("successive devel releases share an audit identity")
	}
	if got := ReleaseID(&Release{TagName: "v1.2.3"}); got != "v1.2.3" {
		t.Fatalf("stable release ID = %q", got)
	}
}

func TestCompareVersionsRejectsDowngrades(t *testing.T) {
	for _, test := range []struct {
		left, right string
		want        int
	}{
		{left: "v1.2.4", right: "1.2.3", want: 1},
		{left: "1.2.3", right: "v1.2.3", want: 0},
		{left: "v1.2.2", right: "1.2.3", want: -1},
		{left: "202512051430-beta", right: "202512041430-beta", want: 1},
	} {
		if got := compareVersions(test.left, test.right); got != test.want {
			t.Errorf("compareVersions(%q, %q) = %d; want %d", test.left, test.right, got, test.want)
		}
	}
}

func TestSelectReleaseFollowsCumulativeChannelsWithoutDowngrading(t *testing.T) {
	currentStable := Release{TagName: "v1.2.3", PublishedAt: time.Unix(100, 0)}
	newerStable := Release{TagName: "v1.2.4", PublishedAt: time.Unix(200, 0)}
	newerBeta := Release{TagName: "202512051430-beta", Prerelease: true, PublishedAt: time.Unix(300, 0)}
	devel := Release{TagName: "devel", Prerelease: true, PublishedAt: time.Unix(400, 0)}

	if got := selectRelease([]Release{newerStable}, "v1.2.3", "stable", 0); got == nil || got.TagName != newerStable.TagName {
		t.Fatalf("stable channel selected %#v, want %s", got, newerStable.TagName)
	}
	if got := selectRelease([]Release{currentStable}, "v1.2.4", "stable", 0); got != nil {
		t.Fatalf("stable channel offered downgrade %#v", got)
	}
	if got := selectRelease([]Release{newerBeta, currentStable}, currentStable.TagName, "beta", 0); got == nil || got.TagName != newerBeta.TagName {
		t.Fatalf("beta channel selected %#v, want newest beta", got)
	}
	if got := selectRelease([]Release{newerBeta, currentStable}, currentStable.TagName, "stable", 0); got != nil {
		t.Fatalf("stable channel selected prerelease %#v", got)
	}
	if got := selectRelease([]Release{devel, newerBeta, currentStable}, currentStable.TagName, "devel", 350); got == nil || got.TagName != devel.TagName {
		t.Fatalf("devel channel selected %#v, want newer rolling release", got)
	}
	if got := selectRelease([]Release{devel, currentStable}, currentStable.TagName, "devel", 450); got != nil {
		t.Fatalf("devel channel offered older rolling release %#v", got)
	}
}

func TestSelectReleaseHonorsDeferralAndKeepsDevelImmediate(t *testing.T) {
	now := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)
	current := Release{TagName: "v1.2.2", PublishedAt: now.Add(-100 * 24 * time.Hour)}
	eligibleStable := Release{TagName: "v1.2.3", PublishedAt: now.Add(-40 * 24 * time.Hour)}
	freshStable := Release{TagName: "v1.2.4", PublishedAt: now.Add(-5 * 24 * time.Hour)}
	freshBeta := Release{TagName: "202611261100-beta", Prerelease: true, PublishedAt: now.Add(-time.Hour)}
	devel := Release{TagName: "devel", Prerelease: true, PublishedAt: now.Add(-time.Hour)}

	if got := selectReleaseWithDefer([]Release{freshStable, eligibleStable, current}, current.TagName, "stable", 0, 30, now); got == nil || got.TagName != eligibleStable.TagName {
		t.Fatalf("stable deferred selection = %#v, want %s", got, eligibleStable.TagName)
	}
	if got := selectReleaseWithDefer([]Release{freshBeta, eligibleStable, current}, current.TagName, "beta", 0, 30, now); got == nil || got.TagName != eligibleStable.TagName {
		t.Fatalf("beta deferred cumulative selection = %#v, want %s", got, eligibleStable.TagName)
	}
	if got := selectReleaseWithDefer([]Release{devel, current}, current.TagName, "devel", now.Add(-24*time.Hour).Unix(), 365, now); got == nil || got.TagName != devel.TagName {
		t.Fatalf("devel selection applied defer window: %#v", got)
	}
}

func TestParseChecksumAndVerify(t *testing.T) {
	content := []byte("release binary")
	sum := sha256.Sum256(content)
	digest := hex.EncodeToString(sum[:])
	got, err := parseChecksum(digest+"  mail-linux-amd64\n", "mail-linux-amd64")
	if err != nil || got != digest {
		t.Fatalf("parseChecksum() = %q, %v", got, err)
	}
	path := filepath.Join(t.TempDir(), "binary")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verify(path, digest); err != nil {
		t.Fatalf("verify() error: %v", err)
	}
	if err := verify(path, strings.Repeat("0", 64)); err == nil {
		t.Fatal("verify accepted incorrect digest")
	}
}

func TestSaveBranchPreservesOtherConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.yml")
	if err := os.WriteFile(path, []byte("server:\n  port: 6443\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveBranch(path, "daily"); err != nil {
		t.Fatal(err)
	}
	if got := ReadBranch(path); got != "devel" {
		t.Fatalf("ReadBranch() = %q; want devel", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "port: 6443") {
		t.Fatalf("unrelated config was lost: %s", data)
	}
}

func TestSaveBranchRejectsInvalidConfigurationAndReadBranchFallsBack(t *testing.T) {
	for _, content := range []string{"server: not-a-map\n", "server:\n  update: false\n", "[]\n"} {
		path := filepath.Join(t.TempDir(), "server.yml")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := SaveBranch(path, "beta"); err == nil {
			t.Errorf("SaveBranch accepted invalid config %q", content)
		}
	}
	if err := SaveBranch(filepath.Join(t.TempDir(), "empty", "server.yml"), "stable"); err != nil {
		t.Fatalf("SaveBranch could not create absent config: %v", err)
	}
	for _, content := range []string{"not: [valid", "server:\n  update:\n    branch: nightly\n"} {
		path := filepath.Join(t.TempDir(), "server.yml")
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := ReadBranch(path); got != "stable" {
			t.Errorf("ReadBranch(%q) = %q, want stable fallback", content, got)
		}
	}
	if got := ReadBranch(filepath.Join(t.TempDir(), "missing.yml")); got != "stable" {
		t.Fatalf("ReadBranch(missing) = %q, want stable", got)
	}
}

func TestCheckWithDeferRejectsInvalidBoundsBeforeNetworkAccess(t *testing.T) {
	for _, days := range []int{-1, 366} {
		if _, err := CheckWithDefer(context.Background(), "v1.2.3", "stable", 0, days, time.Now()); err == nil {
			t.Errorf("CheckWithDefer accepted defer_days=%d", days)
		}
	}
	if _, err := CheckWithDefer(context.Background(), "v1.2.3", "invalid", 0, 0, time.Now()); err == nil {
		t.Fatal("CheckWithDefer accepted an unsupported channel")
	}
}

func TestReleaseURLsRequiresOfficialHTTPSAssets(t *testing.T) {
	release := &Release{Assets: []Asset{{Name: "mail-linux-amd64", URL: "https://github.com/apimgr/mail/releases/download/v1/mail-linux-amd64"}, {Name: "sha256.txt", URL: "https://github.com/apimgr/mail/releases/download/v1/sha256.txt"}}}
	binaryURL, checksumURL, err := releaseURLs(release, "mail-linux-amd64")
	if err != nil || binaryURL == "" || checksumURL == "" {
		t.Fatalf("releaseURLs() = %q, %q, %v", binaryURL, checksumURL, err)
	}
	release.Assets[0].URL = "http://github.com/apimgr/mail/binary"
	if _, _, err := releaseURLs(release, "mail-linux-amd64"); err == nil {
		t.Fatal("releaseURLs accepted an insecure asset URL")
	}
}

func TestApplyStagedRejectsNonNewerReleaseBeforeNetworkAccess(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "version"), []byte("v1.2.3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyStaged(context.Background(), dir, filepath.Join(dir, "current"), "v1.2.3", 1); err == nil {
		t.Fatal("ApplyStaged accepted a release equal to the running version")
	}
}
