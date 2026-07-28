package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	latestReleaseURL = "https://api.github.com/repos/fengqi-dev/kube-clash/releases/latest"
	releasesPageURL  = "https://github.com/fengqi-dev/kube-clash/releases"
)

type Info struct {
	CurrentVersion string    `json:"currentVersion"`
	LatestVersion  string    `json:"latestVersion,omitempty"`
	Available      bool      `json:"available"`
	URL            string    `json:"url"`
	PublishedAt    time.Time `json:"publishedAt,omitempty"`
	CheckedAt      time.Time `json:"checkedAt"`
	Error          string    `json:"error,omitempty"`
}

type Checker struct {
	CurrentVersion string
	HTTPClient     *http.Client
	LatestURL      string
}

func (c *Checker) Check(ctx context.Context) (Info, error) {
	current := c.CurrentVersion
	if current == "" {
		current = "dev"
	}
	info := Info{
		CurrentVersion: current,
		URL:            releasesPageURL,
		CheckedAt:      time.Now(),
	}
	url := c.LatestURL
	if url == "" {
		url = latestReleaseURL
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return info, fmt.Errorf("create update request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "kube-clash/"+current)

	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return info, fmt.Errorf("check for updates: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return info, errors.New("no published Kube Clash release was found")
	}
	if response.StatusCode != http.StatusOK {
		return info, fmt.Errorf("check for updates: GitHub returned %s", response.Status)
	}
	var release struct {
		TagName     string    `json:"tag_name"`
		HTMLURL     string    `json:"html_url"`
		PublishedAt time.Time `json:"published_at"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 2<<20))
	if err := decoder.Decode(&release); err != nil {
		return info, fmt.Errorf("decode latest release: %w", err)
	}
	if release.TagName == "" {
		return info, errors.New("latest GitHub release has no version tag")
	}
	info.LatestVersion = release.TagName
	info.PublishedAt = release.PublishedAt
	if strings.HasPrefix(release.HTMLURL, "https://github.com/fengqi-dev/kube-clash/") {
		info.URL = release.HTMLURL
	}
	if _, err := parseVersion(current); err == nil {
		info.Available = compareVersions(release.TagName, current) > 0
	}
	return info, nil
}

type version struct {
	numbers    [3]int
	prerelease []string
}

func compareVersions(left, right string) int {
	leftVersion, leftErr := parseVersion(left)
	rightVersion, rightErr := parseVersion(right)
	if leftErr != nil || rightErr != nil {
		return 0
	}
	for index := range leftVersion.numbers {
		if leftVersion.numbers[index] < rightVersion.numbers[index] {
			return -1
		}
		if leftVersion.numbers[index] > rightVersion.numbers[index] {
			return 1
		}
	}
	switch {
	case len(leftVersion.prerelease) == 0 && len(rightVersion.prerelease) > 0:
		return 1
	case len(leftVersion.prerelease) > 0 && len(rightVersion.prerelease) == 0:
		return -1
	}
	for index := 0; index < len(leftVersion.prerelease) && index < len(rightVersion.prerelease); index++ {
		comparison := compareIdentifier(leftVersion.prerelease[index], rightVersion.prerelease[index])
		if comparison != 0 {
			return comparison
		}
	}
	switch {
	case len(leftVersion.prerelease) < len(rightVersion.prerelease):
		return -1
	case len(leftVersion.prerelease) > len(rightVersion.prerelease):
		return 1
	default:
		return 0
	}
}

func parseVersion(value string) (version, error) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	value = strings.SplitN(value, "+", 2)[0]
	parts := strings.SplitN(value, "-", 2)
	numbers := strings.Split(parts[0], ".")
	if len(numbers) < 1 || len(numbers) > 3 {
		return version{}, errors.New("invalid version")
	}
	var parsed version
	for index, number := range numbers {
		if number == "" {
			return version{}, errors.New("invalid version")
		}
		item, err := strconv.Atoi(number)
		if err != nil || item < 0 {
			return version{}, errors.New("invalid version")
		}
		parsed.numbers[index] = item
	}
	if len(parts) == 2 {
		if parts[1] == "" {
			return version{}, errors.New("invalid prerelease")
		}
		parsed.prerelease = strings.Split(parts[1], ".")
	}
	return parsed, nil
}

func compareIdentifier(left, right string) int {
	leftNumber, leftErr := strconv.Atoi(left)
	rightNumber, rightErr := strconv.Atoi(right)
	switch {
	case leftErr == nil && rightErr == nil:
		if leftNumber < rightNumber {
			return -1
		}
		if leftNumber > rightNumber {
			return 1
		}
		return 0
	case leftErr == nil:
		return -1
	case rightErr == nil:
		return 1
	default:
		return strings.Compare(left, right)
	}
}
