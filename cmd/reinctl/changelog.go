package main

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Danidani23/agents-rein/internal/adapters/execute"
)

// conventional matches "type(scope)!: description".
var conventional = regexp.MustCompile(`^([a-z]+)(?:\(([^)]+)\))?(!)?:\s*(.+)$`)

// commit is one commit as git log gives it.
type commit struct {
	hash, subject, body string
}

// entry is a commit as the changelog shows it.
type entry struct {
	hash, scope, description string
}

// groups are the changelog's sections.
type groups struct {
	breaking, feat, fix, perf []entry
	other                     int
}

func (grouped groups) tally() tally {
	return tally{breaking: len(grouped.breaking), feat: len(grouped.feat), fix: len(grouped.fix), perf: len(grouped.perf), other: grouped.other}
}

// classify sorts commits into the changelog's sections. A breaking change
// is a ! after the type or a BREAKING CHANGE: footer, whatever the type.
func classify(commits []commit) groups {
	var grouped groups
	for _, current := range commits {
		match := conventional.FindStringSubmatch(current.subject)
		listed := entry{hash: current.hash, description: current.subject}
		kind := ""
		if match != nil {
			kind, listed.scope, listed.description = match[1], match[2], match[4]
		}
		breaking := (match != nil && match[3] == "!") || strings.Contains(current.body, "BREAKING CHANGE:")
		switch {
		case breaking:
			grouped.breaking = append(grouped.breaking, listed)
		case kind == "feat":
			grouped.feat = append(grouped.feat, listed)
		case kind == "fix":
			grouped.fix = append(grouped.fix, listed)
		case kind == "perf":
			grouped.perf = append(grouped.perf, listed)
		default:
			grouped.other++
		}
	}
	return grouped
}

// section renders one release of the changelog.
func section(release version, date string, grouped groups) string {
	var text strings.Builder
	fmt.Fprintf(&text, "## [%s] - %s\n", release.plain(), date)
	for _, part := range []struct {
		title   string
		entries []entry
	}{
		{"⚠ BREAKING CHANGES", grouped.breaking},
		{"Features", grouped.feat},
		{"Bug Fixes", grouped.fix},
		{"Performance", grouped.perf},
	} {
		if len(part.entries) == 0 {
			continue
		}
		fmt.Fprintf(&text, "\n### %s\n\n", part.title)
		for _, listed := range part.entries {
			scope := ""
			if listed.scope != "" {
				scope = "**" + listed.scope + ":** "
			}
			fmt.Fprintf(&text, "- %s%s (%s)\n", scope, listed.description, listed.hash)
		}
	}
	return text.String()
}

// prependSection puts a new section under the file's "# Changelog" heading;
// a missing file starts with the heading.
func prependSection(existing string, newSection string) string {
	const heading = "# Changelog\n"
	if !strings.HasPrefix(existing, heading) {
		return heading + "\n" + newSection + "\n" + existing
	}
	rest := strings.TrimLeft(strings.TrimPrefix(existing, heading), "\n")
	return heading + "\n" + newSection + "\n" + rest
}

// releasePlan is what the next release would be; last is the zero version
// when there has been no release yet.
type releasePlan struct {
	last    version
	first   bool
	next    version
	groups  groups
	section string
}

// lastLabel names what the release is measured against.
func (plan releasePlan) lastLabel() string {
	if plan.first {
		return "the start"
	}
	return plan.last.String()
}

// planRelease reads the local tags and the commits since the last release.
// Without a tag every commit counts, and the release is the first one.
func planRelease(root string, now time.Time) (releasePlan, error) {
	tagList, err := capture(root, "git", "tag", "-l", "v*")
	if err != nil {
		return releasePlan{}, err
	}
	last, found := latestRelease(strings.Split(tagList, "\n"))
	since := ""
	if found {
		since = last.String()
	}
	commits, err := commitsSince(root, since)
	if err != nil {
		return releasePlan{}, err
	}
	grouped := classify(commits)
	plan := releasePlan{last: last, first: !found, groups: grouped}
	if grouped.tally().releasable() {
		plan.next = nextVersion(last, grouped.tally())
		if plan.first {
			plan.next = firstRelease
		}
		plan.section = section(plan.next, now.Format("2006-01-02"), grouped)
	}
	return plan, nil
}

// commitsSince lists the commits on HEAD's first-parent line since the tag,
// all of them when tag is empty, newest first, merges left out.
func commitsSince(root string, tag string) ([]commit, error) {
	revisions := "HEAD"
	if tag != "" {
		revisions = tag + "..HEAD"
	}
	// a unit separator between fields, a record separator between commits:
	// subjects and bodies are free text
	raw, err := capture(root, "git", "log", "--first-parent", "--no-merges", "--format=%h%x1f%s%x1f%b%x1e", revisions)
	if err != nil {
		return nil, err
	}
	var commits []commit
	for _, record := range strings.Split(raw, "\x1e") {
		record = strings.TrimSpace(record)
		if record == "" {
			continue
		}
		fields := strings.SplitN(record, "\x1f", 3)
		for len(fields) < 3 {
			fields = append(fields, "")
		}
		commits = append(commits, commit{hash: fields[0], subject: fields[1], body: fields[2]})
	}
	return commits, nil
}

// capture runs a command in root and returns its trimmed output.
func capture(root string, name string, arguments ...string) (string, error) {
	result, err := execute.Run(root, name, arguments...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(result.Stdout), nil
}
