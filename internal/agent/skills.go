package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/solarisjon/phoenix/internal/model"
	"github.com/solarisjon/phoenix/internal/provider"
	"github.com/solarisjon/phoenix/internal/store"
)

var (
	skillIntentPattern  = regexp.MustCompile(`(?i)(?:run|execute|use|invoke|apply)\s+(?:the\s+)?(?:skill\s+(?:called\s+)?)?[\w-]+(?:\s+skill)?`)
	skillSlugInvalidChars = regexp.MustCompile(`[^a-z0-9]+`)
)

// SkillImportResult summarises a filesystem skill import run.
type SkillImportResult struct {
	Imported int            `json:"imported"`
	Updated  int            `json:"updated"`
	Skipped  int            `json:"skipped"`
	Skills   []*model.Skill `json:"skills"`
	Errors   []string       `json:"errors,omitempty"`
}

// ScannedSkill is a lightweight preview entry returned before import.
type ScannedSkill struct {
	Slug            string `json:"slug"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	SourcePath      string `json:"source_path"`
	AlreadyImported bool   `json:"already_imported"`
}

// TaskRequestsSkillExecution reports whether a task should run in direct skill
// execution mode rather than orchestrator decomposition.
func TaskRequestsSkillExecution(ctx context.Context, repo store.SkillRepo, importDirs []string, workingDir string, t *model.Task, proj *model.Project) bool {
	var allSkills []*model.Skill
	if repo != nil {
		dbSkills, err := repo.ListEnabled(ctx)
		if err == nil {
			allSkills = MergeSkills(dbSkills, DiscoverFilesystemSkills(importDirs, workingDir))
		}
	}
	if len(allSkills) == 0 {
		allSkills = DiscoverFilesystemSkills(importDirs, workingDir)
	}
	return TaskHasSkillIntent(allSkills, t, proj)
}

// SkillHaystack returns the lowercased text used to match skill slugs against a
// task or monitor objective.
func SkillHaystack(t *model.Task, proj *model.Project) string {
	haystack := strings.ToLower(t.Title + " " + t.Description)
	if proj != nil {
		haystack += " " + strings.ToLower(proj.Objective)
	}
	return haystack
}

// NormalizeSkillSlug lowercases and normalises a slug token.
func NormalizeSkillSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = skillSlugInvalidChars.ReplaceAllString(s, "_")
	return strings.Trim(s, "_")
}

// ExpandSkillPath expands ~ and ${ENV} placeholders in a configured skill path.
func ExpandSkillPath(path string) string {
	path = strings.TrimSpace(provider.ExpandEnv(path))
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	return filepath.Clean(path)
}

// SkillSearchRoots returns deduplicated directories to scan for skills.
func SkillSearchRoots(importDirs []string, workingDir string) []string {
	seen := make(map[string]bool)
	var roots []string
	add := func(path string) {
		path = ExpandSkillPath(path)
		if path == "" {
			return
		}
		if seen[path] {
			return
		}
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			return
		}
		seen[path] = true
		roots = append(roots, path)
	}

	for _, dir := range importDirs {
		add(dir)
	}
	if home, err := os.UserHomeDir(); err == nil {
		add(filepath.Join(home, ".agents", "skills"))
		add(filepath.Join(home, ".cursor", "skills-cursor"))
	}
	if workingDir != "" {
		add(filepath.Join(workingDir, ".agents", "skills"))
		add(filepath.Join(workingDir, ".cursor", "skills"))
	}
	return roots
}

// ExpandSkillDirs expands configured import paths without adding defaults.
func ExpandSkillDirs(dirs []string) []string {
	seen := make(map[string]bool)
	var roots []string
	for _, dir := range dirs {
		path := ExpandSkillPath(dir)
		if path == "" || seen[path] {
			continue
		}
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			continue
		}
		seen[path] = true
		roots = append(roots, path)
	}
	return roots
}

// slugMatches reports whether haystack references the given skill slug, treating
// underscores and hyphens as interchangeable.
func slugMatches(haystack, slug string) bool {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "" {
		return false
	}
	if strings.Contains(haystack, slug) {
		return true
	}
	alt := strings.ReplaceAll(slug, "_", "-")
	if alt != slug && strings.Contains(haystack, alt) {
		return true
	}
	alt = strings.ReplaceAll(slug, "-", "_")
	return alt != slug && strings.Contains(haystack, alt)
}

// MatchSkills returns enabled skills relevant to this task: bound as the
// project's default, or mentioned by slug in the task/monitor text.
func MatchSkills(allSkills []*model.Skill, t *model.Task, proj *model.Project) []*model.Skill {
	haystack := SkillHaystack(t, proj)

	seen := make(map[string]bool)
	var matched []*model.Skill
	add := func(sk *model.Skill) {
		if sk == nil || !sk.Enabled || seen[sk.ID] {
			return
		}
		seen[sk.ID] = true
		matched = append(matched, sk)
	}
	for _, sk := range allSkills {
		if proj != nil && proj.DefaultSkillID != nil && *proj.DefaultSkillID == sk.ID {
			add(sk)
			continue
		}
		if slugMatches(haystack, sk.Slug) {
			add(sk)
		}
	}
	return matched
}

// TaskHasSkillIntent reports whether the task or project is asking Phoenix to
// run a skill, even when no skill definition has been registered yet.
func TaskHasSkillIntent(allSkills []*model.Skill, t *model.Task, proj *model.Project) bool {
	if proj != nil && proj.DefaultSkillID != nil && strings.TrimSpace(*proj.DefaultSkillID) != "" {
		return true
	}
	if len(MatchSkills(allSkills, t, proj)) > 0 {
		return true
	}
	return skillIntentPattern.MatchString(SkillHaystack(t, proj))
}

// MergeSkills combines DB and filesystem skills, preferring DB entries when
// slugs collide.
func MergeSkills(dbSkills, fsSkills []*model.Skill) []*model.Skill {
	bySlug := make(map[string]*model.Skill, len(dbSkills)+len(fsSkills))
	order := make([]string, 0, len(dbSkills)+len(fsSkills))
	add := func(sk *model.Skill) {
		if sk == nil || !sk.Enabled {
			return
		}
		slug := strings.ToLower(strings.TrimSpace(sk.Slug))
		if slug == "" {
			return
		}
		if _, exists := bySlug[slug]; !exists {
			order = append(order, slug)
		}
		bySlug[slug] = sk
	}
	for _, sk := range fsSkills {
		add(sk)
	}
	for _, sk := range dbSkills {
		add(sk)
	}
	out := make([]*model.Skill, 0, len(order))
	for _, slug := range order {
		out = append(out, bySlug[slug])
	}
	return out
}

// ResolveSkills loads enabled DB skills and discovers matching filesystem
// skills for the task's working directory.
func ResolveSkills(ctx context.Context, repo store.SkillRepo, importDirs []string, workingDir string, t *model.Task, proj *model.Project) ([]*model.Skill, error) {
	var dbSkills []*model.Skill
	if repo != nil {
		var err error
		dbSkills, err = repo.ListEnabled(ctx)
		if err != nil {
			return nil, err
		}
	}
	all := MergeSkills(dbSkills, DiscoverFilesystemSkills(importDirs, workingDir))
	return MatchSkills(all, t, proj), nil
}

// DiscoverFilesystemSkills scans configured and default skill directories.
func DiscoverFilesystemSkills(importDirs []string, workingDir string) []*model.Skill {
	return discoverSkillsInRoots(SkillSearchRoots(importDirs, workingDir))
}

// DiscoverFilesystemSkillsIn scans only the given directories.
func DiscoverFilesystemSkillsIn(dirs []string) []*model.Skill {
	return discoverSkillsInRoots(ExpandSkillDirs(dirs))
}

type filesystemSkillEntry struct {
	Skill      *model.Skill
	SourcePath string
}

func discoverSkillsInRoots(roots []string) []*model.Skill {
	seen := make(map[string]bool)
	var out []*model.Skill
	for _, root := range roots {
		for _, entry := range scanSkillRoot(root) {
			slug := strings.ToLower(entry.Skill.Slug)
			if seen[slug] {
				continue
			}
			seen[slug] = true
			out = append(out, entry.Skill)
		}
	}
	return out
}

// scanSkillRoot reads skills from a directory. The path may itself contain
// SKILL.md, or it may be a container whose immediate child directories each
// contain SKILL.md.
func scanSkillRoot(root string) []filesystemSkillEntry {
	var out []filesystemSkillEntry
	rootSkillPath := filepath.Join(root, "SKILL.md")
	if sk, err := parseFilesystemSkill(rootSkillPath, filepath.Base(root)); err == nil && sk != nil {
		out = append(out, filesystemSkillEntry{Skill: sk, SourcePath: rootSkillPath})
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return out
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillPath := filepath.Join(root, entry.Name(), "SKILL.md")
		sk, err := parseFilesystemSkill(skillPath, entry.Name())
		if err != nil || sk == nil {
			continue
		}
		out = append(out, filesystemSkillEntry{Skill: sk, SourcePath: skillPath})
	}
	return out
}

func parseFilesystemSkill(path, dirName string) (*model.Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	name, description, slug, body := parseSkillMarkdown(string(data))
	if slug == "" {
		slug = dirName
	}
	slug = NormalizeSkillSlug(slug)
	if slug == "" {
		return nil, fmt.Errorf("invalid slug")
	}
	if name == "" {
		name = dirName
	}
	if strings.TrimSpace(body) == "" {
		return nil, fmt.Errorf("empty skill body")
	}
	return &model.Skill{
		ID:           "fs:" + slug,
		Name:         name,
		Slug:         slug,
		Description:  description,
		Instructions: body,
		Enabled:      true,
	}, nil
}

func parseSkillMarkdown(raw string) (name, description, slug, body string) {
	content := strings.TrimSpace(raw)
	if !strings.HasPrefix(content, "---") {
		return "", "", "", content
	}
	end := strings.Index(content[3:], "---")
	if end == -1 {
		return "", "", "", content
	}
	frontmatter := content[3 : 3+end]
	body = strings.TrimSpace(content[3+end+3:])
	for _, line := range strings.Split(frontmatter, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		switch key {
		case "name":
			name = val
			if slug == "" {
				slug = val
			}
		case "description":
			description = val
		}
	}
	return name, description, slug, body
}

// ScanFilesystemSkills discovers skills under dirs and reports whether each
// slug is already present in the database.
func ScanFilesystemSkills(ctx context.Context, repo store.SkillRepo, dirs []string) ([]ScannedSkill, error) {
	seen := make(map[string]bool)
	var out []ScannedSkill
	for _, root := range ExpandSkillDirs(dirs) {
		for _, entry := range scanSkillRoot(root) {
			slug := strings.ToLower(entry.Skill.Slug)
			if seen[slug] {
				continue
			}
			seen[slug] = true
			scanned := ScannedSkill{
				Slug:        entry.Skill.Slug,
				Name:        entry.Skill.Name,
				Description: entry.Skill.Description,
				SourcePath:  entry.SourcePath,
			}
			if repo != nil {
				if existing, err := repo.GetBySlug(ctx, slug); err == nil && existing != nil {
					scanned.AlreadyImported = true
				}
			}
			out = append(out, scanned)
		}
	}
	return out, nil
}

func slugFilter(slugs []string) map[string]bool {
	if len(slugs) == 0 {
		return nil
	}
	filter := make(map[string]bool, len(slugs))
	for _, slug := range slugs {
		if s := NormalizeSkillSlug(slug); s != "" {
			filter[s] = true
		}
	}
	return filter
}

// ImportFilesystemSkills upserts selected skills discovered under the given
// directories into the Phoenix skills table. When slugs is non-empty, only
// those slugs are imported.
func ImportFilesystemSkills(ctx context.Context, repo store.SkillRepo, dirs []string, slugs []string, overwrite bool) (*SkillImportResult, error) {
	if repo == nil {
		return nil, fmt.Errorf("skill repository unavailable")
	}
	filter := slugFilter(slugs)
	if filter != nil && len(filter) == 0 {
		return nil, fmt.Errorf("no valid skill slugs selected")
	}
	result := &SkillImportResult{}
	seen := make(map[string]bool)
	for _, root := range ExpandSkillDirs(dirs) {
		for _, entry := range scanSkillRoot(root) {
			fsSkill := entry.Skill
			slug := strings.ToLower(fsSkill.Slug)
			if seen[slug] {
				continue
			}
			if filter != nil && !filter[slug] {
				continue
			}
			seen[slug] = true

			existing, err := repo.GetBySlug(ctx, slug)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("%s: lookup failed: %v", slug, err))
				continue
			}
			if existing != nil && !overwrite {
				result.Skipped++
				result.Skills = append(result.Skills, existing)
				continue
			}

			now := time.Now().UTC()
			sk := &model.Skill{
				ID:           uuid.New().String(),
				Name:         fsSkill.Name,
				Slug:         slug,
				Description:  fsSkill.Description,
				Instructions: fsSkill.Instructions,
				Enabled:      true,
				CreatedAt:    now,
			}
			if existing != nil {
				sk.ID = existing.ID
				sk.CreatedAt = existing.CreatedAt
				if err := repo.Update(ctx, sk); err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("%s: update failed: %v", slug, err))
					continue
				}
				result.Updated++
			} else {
				if err := repo.Create(ctx, sk); err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("%s: create failed: %v", slug, err))
					continue
				}
				result.Imported++
			}
			result.Skills = append(result.Skills, sk)
		}
	}
	return result, nil
}

// SkillExecutionModeSection returns instructions that override routing/coordination
// personas so the assigned agent executes skill work directly.
func SkillExecutionModeSection(matched []*model.Skill, haystack string) string {
	var b strings.Builder
	b.WriteString("## Skill Execution Mode\n\n")
	b.WriteString("This task is a Phoenix skill execution. You MUST carry out the skill instructions yourself.\n")
	b.WriteString("Do NOT route, delegate, decompose, or spawn subtasks for this work.\n")
	b.WriteString("Ignore any orchestrator, coordinator, or routing-only behaviour in your persona — skill execution takes precedence.\n")
	b.WriteString("Follow the skill instructions below as your primary task.\n")
	if len(matched) == 0 && skillIntentPattern.MatchString(haystack) {
		b.WriteString("\nA skill was requested, but no matching skill definition was found.\n")
		b.WriteString("Register the skill under Settings → Plugins → Skills, import it from a configured directory, or install it at ~/.agents/skills/<slug>/SKILL.md, then retry.\n")
	}
	return b.String()
}

// InjectSkillExecutionMode prepends skill execution instructions so they override
// routing-focused agent behaviour.
func InjectSkillExecutionMode(req provider.TaskRequest, matched []*model.Skill, haystack string) provider.TaskRequest {
	section := SkillExecutionModeSection(matched, haystack)
	req.SystemPrompt = section + "\n" + req.SystemPrompt
	return req
}
