package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
)

type semanticVersion struct {
	major      int
	minor      int
	patch      int
	prerelease string
}

func parseSemanticVersion(tag string) (semanticVersion, bool) {
	cleaned := strings.TrimSpace(tag)
	cleaned = strings.TrimPrefix(cleaned, "tag/")
	cleaned = strings.TrimPrefix(cleaned, "v")
	if cleaned == "" {
		return semanticVersion{}, false
	}
	if plus := strings.Index(cleaned, "+"); plus >= 0 {
		cleaned = cleaned[:plus]
	}

	core := cleaned
	pre := ""
	if dash := strings.Index(cleaned, "-"); dash >= 0 {
		core = cleaned[:dash]
		pre = strings.TrimSpace(cleaned[dash+1:])
	}

	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return semanticVersion{}, false
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return semanticVersion{}, false
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return semanticVersion{}, false
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return semanticVersion{}, false
	}

	return semanticVersion{
		major:      major,
		minor:      minor,
		patch:      patch,
		prerelease: pre,
	}, true
}

func compareSemanticVersion(left, right semanticVersion) int {
	if left.major != right.major {
		return left.major - right.major
	}
	if left.minor != right.minor {
		return left.minor - right.minor
	}
	if left.patch != right.patch {
		return left.patch - right.patch
	}
	if left.prerelease == right.prerelease {
		return 0
	}
	if left.prerelease == "" {
		return 1
	}
	if right.prerelease == "" {
		return -1
	}
	return strings.Compare(left.prerelease, right.prerelease)
}

func selectHighestReleaseTag(tags []string) (string, bool) {
	bestTag := ""
	var bestVersion semanticVersion
	found := false

	for _, candidate := range tags {
		version, ok := parseSemanticVersion(candidate)
		if !ok {
			continue
		}
		if !found || compareSemanticVersion(version, bestVersion) > 0 {
			bestTag = candidate
			bestVersion = version
			found = true
		}
	}

	return bestTag, found
}

func fetchLatestReleaseTag(ctx context.Context) (string, error) {
	if tag, err := fetchLatestReleaseTagFromRedirect(ctx); err == nil {
		return tag, nil
	}

	return fetchLatestReleaseTagFromAPI(ctx)
}

func fetchLatestReleaseTagFromRedirect(ctx context.Context) (string, error) {
	const latestReleaseURL = "https://github.com/evansrrr/BKNetwork/releases/latest"

	client := &http.Client{Timeout: timeoutMedium}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "BKNetwork-Version-Checker")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.Request == nil || resp.Request.URL == nil {
		return "", errors.New("missing redirect target url")
	}

	tag := strings.TrimSpace(path.Base(resp.Request.URL.Path))
	if tag == "" || strings.EqualFold(tag, "latest") {
		return "", errors.New("invalid redirect tag")
	}
	return tag, nil
}

func fetchLatestReleaseTagFromAPI(ctx context.Context) (string, error) {
	const releasesAPI = "https://api.github.com/repos/evansrrr/BKNetwork/releases?per_page=100"

	client := &http.Client{Timeout: timeoutMedium}
	apiReq, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesAPI, nil)
	if err != nil {
		return "", err
	}
	apiReq.Header.Set("Accept", "application/vnd.github+json")
	apiReq.Header.Set("User-Agent", "BKNetwork-Version-Checker")

	resp, err := client.Do(apiReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("github api returned status %d", resp.StatusCode)
	}

	var payload []struct {
		TagName    string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}

	stableTags := make([]string, 0, len(payload))
	allTags := make([]string, 0, len(payload))
	for _, item := range payload {
		tag := strings.TrimSpace(item.TagName)
		if tag == "" || item.Draft {
			continue
		}
		allTags = append(allTags, tag)
		if !item.Prerelease {
			stableTags = append(stableTags, tag)
		}
	}

	if tag, ok := selectHighestReleaseTag(stableTags); ok {
		return tag, nil
	}
	if tag, ok := selectHighestReleaseTag(allTags); ok {
		return tag, nil
	}

	if len(allTags) > 0 {
		return allTags[0], nil
	}

	if len(payload) == 0 {
		return "", errors.New("empty release list from github")
	}

	first := strings.TrimSpace(payload[0].TagName)
	if first == "" {
		return "", errors.New("empty tag from github")
	}
	return first, nil
}
