package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"os"
	"sort"
	"strings"
	"text/template"
	"time"
)

// graphQLQuery fetches 30-day contribution totals and daily counts, repository counts across contribution types, and per-repository
// commit counts and primary languages for the active stack. Private repositories are only included when the token
// has the repo:status, read:user and read:org scopes; otherwise they are hidden in restrictedContributionsCount.
const graphQLQuery = `
query($login: String!, $from: DateTime!, $to: DateTime!) {
  user(login: $login) {
    contributionsCollection(from: $from, to: $to) {
      restrictedContributionsCount
      contributionCalendar {
        totalContributions
        weeks {
          contributionDays {
            date
            contributionCount
          }
        }
      }
      totalRepositoriesWithContributedCommits
      totalRepositoriesWithContributedPullRequests
      totalRepositoriesWithContributedPullRequestReviews
      totalRepositoriesWithContributedIssues
      commitContributionsByRepository(maxRepositories: 100) {
        repository {
          name
          nameWithOwner
          primaryLanguage {
            name
          }
        }
        contributions(first: 100) {
          nodes {
            commitCount
          }
        }
      }
      pullRequestContributionsByRepository(maxRepositories: 100) {
        repository {
          nameWithOwner
        }
      }
      pullRequestReviewContributionsByRepository(maxRepositories: 100) {
        repository {
          nameWithOwner
        }
      }
      issueContributionsByRepository(maxRepositories: 100) {
        repository {
          nameWithOwner
        }
      }
    }
  }
}
`

type repoContributionNode struct {
	Repository struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
}

// commitContributionNode is one repository's commit contributions; each contribution node covers a single day.
type commitContributionNode struct {
	Repository struct {
		Name            string `json:"name"`
		NameWithOwner   string `json:"nameWithOwner"`
		PrimaryLanguage *struct {
			Name string `json:"name"`
		} `json:"primaryLanguage"`
	} `json:"repository"`
	Contributions struct {
		Nodes []struct {
			CommitCount int `json:"commitCount"`
		} `json:"nodes"`
	} `json:"contributions"`
}

type telemetryData struct {
	User struct {
		ContributionsCollection struct {
			RestrictedContributionsCount int `json:"restrictedContributionsCount"`
			ContributionCalendar         struct {
				TotalContributions int `json:"totalContributions"`
				Weeks              []struct {
					ContributionDays []struct {
						Date              string `json:"date"`
						ContributionCount int    `json:"contributionCount"`
					} `json:"contributionDays"`
				} `json:"weeks"`
			} `json:"contributionCalendar"`
			TotalRepositoriesWithContributedCommits            int                      `json:"totalRepositoriesWithContributedCommits"`
			TotalRepositoriesWithContributedPullRequests       int                      `json:"totalRepositoriesWithContributedPullRequests"`
			TotalRepositoriesWithContributedPullRequestReviews int                      `json:"totalRepositoriesWithContributedPullRequestReviews"`
			TotalRepositoriesWithContributedIssues             int                      `json:"totalRepositoriesWithContributedIssues"`
			CommitContributionsByRepository                    []commitContributionNode `json:"commitContributionsByRepository"`
			PullRequestContributionsByRepository               []repoContributionNode   `json:"pullRequestContributionsByRepository"`
			PullRequestReviewContributionsByRepository         []repoContributionNode   `json:"pullRequestReviewContributionsByRepository"`
			IssueContributionsByRepository                     []repoContributionNode   `json:"issueContributionsByRepository"`
		} `json:"contributionsCollection"`
	} `json:"user"`
}

// TemplateData holds all dynamic values injected into the SVG template, with layout already computed.
type TemplateData struct {
	Contributions30d int
	Repositories30d  int
	Activity         []activityBar
	ActiveDays       int
	TotalDays        int
	Languages        []languageRow
	PromptY          int
	Height           int
	FrameHeight      int
	UpdatedAt        string
}

// activityBar is one day in the activity sparkline.
type activityBar struct {
	X, Y, Height int
	Active       bool
}

// languageRow is one language in the languages chart.
type languageRow struct {
	Name     string
	Percent  int
	BarWidth int
	Y        int
	Delay    string
}

// languageShare is a language's percentage of the commit-weighted total.
type languageShare struct {
	Name    string
	Percent int
}

// Layout of the status report, in SVG units. Fixed positions live in svgTemplate; languageBarMax must match
// the width of the language track rect there.
const (
	activityX         = 170
	activityBaseline  = 114
	activityStep      = 14
	activityMaxHeight = 20
	activityMinHeight = 2
	languagesY        = 190
	languageRowStep   = 24
	languageBarMax    = 280
)

func main() {
	username := getEnv("GITHUB_USERNAME", "RowbDowg")
	token := os.Getenv("GITHUB_TOKEN")
	outputPath := getEnv("OUTPUT_PATH", "terminal.svg")

	if token == "" {
		fmt.Fprintln(os.Stderr, "error: GITHUB_TOKEN environment variable is required")
		os.Exit(1)
	}

	data, err := fetchGitHubTelemetry(username, token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error fetching GitHub data: %v\n", err)
		os.Exit(1)
	}

	if err := renderSVG(outputPath, data); err != nil {
		fmt.Fprintf(os.Stderr, "error rendering SVG: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Successfully generated %s for %s\n", outputPath, username)
}

func fetchGitHubTelemetry(username, token string) (TemplateData, error) {
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	from := today.AddDate(0, 0, -30).Format(time.RFC3339)
	to := today.Add(-time.Second).Format(time.RFC3339)

	var telemetry telemetryData
	if err := postGraphQL(token, graphQLQuery, map[string]any{"login": username, "from": from, "to": to}, &telemetry); err != nil {
		return TemplateData{}, err
	}

	cc := telemetry.User.ContributionsCollection

	// Deduplicate visible repositories across all contribution types.
	uniqueRepos := make(map[string]struct{})
	for _, item := range cc.CommitContributionsByRepository {
		uniqueRepos[item.Repository.NameWithOwner] = struct{}{}
	}
	for _, item := range cc.PullRequestContributionsByRepository {
		uniqueRepos[item.Repository.NameWithOwner] = struct{}{}
	}
	for _, item := range cc.PullRequestReviewContributionsByRepository {
		uniqueRepos[item.Repository.NameWithOwner] = struct{}{}
	}
	for _, item := range cc.IssueContributionsByRepository {
		uniqueRepos[item.Repository.NameWithOwner] = struct{}{}
	}

	// Weight each repository's primary language by the commits made to it in the window.
	langWeights := make(map[string]int)
	for _, item := range cc.CommitContributionsByRepository {
		repo := item.Repository
		if strings.EqualFold(repo.Name, username) || repo.PrimaryLanguage == nil {
			continue
		}
		for _, day := range item.Contributions.Nodes {
			langWeights[repo.PrimaryLanguage.Name] += day.CommitCount
		}
	}

	// Ensure private/restricted repository counts are never undercounted when using a restricted token.
	repoCount := len(uniqueRepos)
	for _, bucketCount := range []int{
		cc.TotalRepositoriesWithContributedCommits,
		cc.TotalRepositoriesWithContributedPullRequests,
		cc.TotalRepositoriesWithContributedPullRequestReviews,
		cc.TotalRepositoriesWithContributedIssues,
	} {
		if bucketCount > repoCount {
			repoCount = bucketCount
		}
	}

	languages, err := topLanguages(langWeights)
	if err != nil {
		return TemplateData{}, err
	}

	// The calendar can include days outside the window, so keep only the 30 days from "from" to yesterday.
	fromDate := from[:len("2006-01-02")]
	toDate := to[:len("2006-01-02")]
	var dailyCounts []int
	for _, week := range cc.ContributionCalendar.Weeks {
		for _, day := range week.ContributionDays {
			if day.Date >= fromDate && day.Date <= toDate {
				dailyCounts = append(dailyCounts, day.ContributionCount)
			}
		}
	}

	return buildTemplateData(cc.ContributionCalendar.TotalContributions, repoCount, dailyCounts, languages), nil
}

// buildTemplateData lays out the status report; the SVG grows taller with each language row.
func buildTemplateData(contributions, repositories int, dailyCounts []int, languages []languageShare) TemplateData {
	data := TemplateData{
		Contributions30d: contributions,
		Repositories30d:  repositories,
		TotalDays:        len(dailyCounts),
		UpdatedAt:        time.Now().UTC().Format("2006-01-02 15:04 UTC"),
	}
	data.Activity, data.ActiveDays = activityBars(dailyCounts)
	data.Languages = languageRows(languages)
	data.PromptY = languagesY + (len(data.Languages)-1)*languageRowStep + 48
	data.Height = data.PromptY + 32
	data.FrameHeight = data.Height - 2
	return data
}

// activityBars scales daily contribution counts into sparkline bars, returning the bars and the number of active days.
func activityBars(dailyCounts []int) ([]activityBar, int) {
	maxCount := 0
	for _, count := range dailyCounts {
		maxCount = max(maxCount, count)
	}

	var bars []activityBar
	activeDays := 0
	for i, count := range dailyCounts {
		height := activityMinHeight
		if count > 0 {
			activeDays++
			height = max(activityMinHeight+1, count*activityMaxHeight/maxCount)
		}
		bars = append(bars, activityBar{
			X:      activityX + i*activityStep,
			Y:      activityBaseline - height,
			Height: height,
			Active: count > 0,
		})
	}
	return bars, activeDays
}

// languageRows lays out one chart row per language, escaping names for the SVG.
func languageRows(shares []languageShare) []languageRow {
	var rows []languageRow
	for i, share := range shares {
		rows = append(rows, languageRow{
			Name:     html.EscapeString(share.Name),
			Percent:  share.Percent,
			BarWidth: max(1, share.Percent*languageBarMax/100),
			Y:        languagesY + i*languageRowStep,
			Delay:    fmt.Sprintf("%.2fs", 0.85+float64(i)*0.15),
		})
	}
	return rows
}

// postGraphQL sends a query to the GitHub GraphQL API and decodes the response data into data.
func postGraphQL(token, query string, variables map[string]any, data any) error {
	bodyBytes, err := json.Marshal(map[string]any{
		"query":     query,
		"variables": variables,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, "https://api.github.com/graphql", bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "RowbDowg-Profile-Renderer")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected GitHub API status: %s", resp.Status)
	}

	var gqlResp struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&gqlResp); err != nil {
		return err
	}
	if len(gqlResp.Errors) > 0 {
		return fmt.Errorf("graphql error: %s", gqlResp.Errors[0].Message)
	}
	return json.Unmarshal(gqlResp.Data, data)
}

// topLanguages returns the five most-committed languages with their share of all commits across languages.
func topLanguages(weights map[string]int) ([]languageShare, error) {
	if len(weights) == 0 {
		return nil, fmt.Errorf("no languages found in repositories committed to in the last 30 days")
	}

	type kv struct {
		Key   string
		Value int
	}
	var sorted []kv
	total := 0
	for k, v := range weights {
		sorted = append(sorted, kv{k, v})
		total += v
	}
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Value == sorted[j].Value {
			return sorted[i].Key < sorted[j].Key
		}
		return sorted[i].Value > sorted[j].Value
	})

	var shares []languageShare
	for i, item := range sorted {
		if i >= 5 {
			break
		}
		shares = append(shares, languageShare{Name: item.Key, Percent: (item.Value*100 + total/2) / total})
	}
	return shares, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

const svgTemplate = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 820 {{ .Height }}" width="820" height="{{ .Height }}">
  <style>
    .bg { fill: #0F172A; stroke: #1E293B; stroke-width: 1.5; }
    .header { fill: #1E293B; }
    .title { fill: #94A3B8; font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; font-size: 13px; }
    .text { font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace; font-size: 13.5px; }
    .prompt { fill: #F97316; font-weight: bold; }
    .cmd { fill: #F8FAFC; }
    .label { fill: #64748B; }
    .num { fill: #FBBF24; }
    .muted { fill: #64748B; font-size: 12px; }
    .spark { fill: #34D399; }
    .spark-idle { fill: #334155; }
    .track { fill: #1E293B; }
    .bar { fill: #38BDF8; }
    .line { opacity: 0; animation: fadeIn 0.25s ease-out forwards; }
    .l1 { animation-delay: 0.15s; }
    .l2 { animation-delay: 0.45s; }
    .l3 { animation-delay: 0.65s; }
    .l8 { animation-delay: 1.75s; }
    .cursor { fill: #F97316; animation: blink 1s step-end infinite; }

    @keyframes fadeIn {
      from { opacity: 0; transform: translateY(3px); }
      to { opacity: 1; transform: translateY(0); }
    }
    @keyframes blink {
      0%, 100% { opacity: 1; }
      50% { opacity: 0; }
    }
  </style>

  <!-- Window Frame -->
  <rect class="bg" x="1" y="1" width="818" height="{{ .FrameHeight }}" rx="10" />
  <path class="header" d="M 11 1 H 809 A 10 10 0 0 1 819 11 V 36 H 1 V 11 A 10 10 0 0 1 11 1 Z" />

  <!-- Window Controls -->
  <circle cx="22" cy="19" r="6" fill="#EF4444" />
  <circle cx="42" cy="19" r="6" fill="#F59E0B" />
  <circle cx="62" cy="19" r="6" fill="#10B981" />
  <text class="title" x="410" y="23" text-anchor="middle">rob@london: ~/craft — zsh</text>

  <!-- Terminal Body -->
  <g class="text">
    <text class="line l1" x="24" y="68">
      <tspan class="prompt">❯ </tspan>
      <tspan class="cmd">rob status --since 30d</tspan>
    </text>

    <!-- Daily activity sparkline -->
    <g class="line l2">
      <text class="label" x="44" y="112">activity</text>
      {{- range .Activity }}
      <rect class="{{ if .Active }}spark{{ else }}spark-idle{{ end }}" x="{{ .X }}" y="{{ .Y }}" width="10" height="{{ .Height }}" rx="1.5" />
      {{- end }}
      <text x="600" y="112"><tspan class="num">{{ .ActiveDays }}</tspan><tspan class="label">/{{ .TotalDays }} days active</tspan></text>
    </g>

    <!-- Totals -->
    <text class="line l3" y="152">
      <tspan class="label" x="44">contributions</tspan><tspan class="num" x="170">{{ .Contributions30d }}</tspan>
      <tspan class="label" x="310">repositories</tspan><tspan class="num" x="430">{{ .Repositories30d }}</tspan>
    </text>

    <!-- Languages by share of commits -->
    <text class="line l3" x="44" y="190"><tspan class="label">languages</tspan></text>
    {{- range .Languages }}
    <g class="line" style="animation-delay: {{ .Delay }}">
      <text class="cmd" x="170" y="{{ .Y }}">{{ .Name }}</text>
      <rect class="track" x="310" y="{{ .Y }}" width="280" height="10" rx="2" transform="translate(0 -10)" />
      <rect class="bar" x="310" y="{{ .Y }}" width="{{ .BarWidth }}" height="10" rx="2" transform="translate(0 -10)" />
      <text class="num" x="640" y="{{ .Y }}" text-anchor="end">{{ .Percent }}%</text>
    </g>
    {{- end }}

    <!-- Active Prompt & Sync Timestamp -->
    <g class="line l8">
      <text class="prompt" x="24" y="{{ .PromptY }}">❯</text>
      <rect class="cursor" x="42" y="{{ .PromptY }}" width="9" height="16" transform="translate(0 -13)" />
      <text class="muted" x="796" y="{{ .PromptY }}" text-anchor="end">synced {{ .UpdatedAt }}</text>
    </g>
  </g>
</svg>
`

func renderSVG(outputPath string, data TemplateData) error {
	tmpl, err := template.New("terminal").Parse(svgTemplate)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return err
	}

	return os.WriteFile(outputPath, buf.Bytes(), 0644)
}
