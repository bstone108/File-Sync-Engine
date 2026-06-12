package scanner

import (
	"bufio"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"filesyncengine/internal/block"
)

type Options struct {
	BlockSize      int
	IgnoreSuffixes []string
}

type Result struct {
	Root         string
	Files        []File
	Inaccessible []InaccessibleFile
}

type InaccessibleFile struct {
	RelativePath string
	Error        string
}

type File struct {
	RelativePath string
	Manifest     block.Manifest
}

func ScanFolder(root string, opts Options) (Result, error) {
	return scanFolder(root, opts, true)
}

func ScanFolderMetadataOnly(root string, opts Options) (Result, error) {
	return scanFolder(root, opts, false)
}

func ScanFile(path string, blockSize int) (block.Manifest, error) {
	return fileManifest(path, blockSize, true)
}

func ScanFileMetadataOnly(path string, blockSize int) (block.Manifest, error) {
	return fileManifest(path, blockSize, false)
}

func scanFolder(root string, opts Options, hashBlocks bool) (Result, error) {
	result := Result{Root: root}
	ignoreRules, err := loadSyncIgnoreRules(root)
	if err != nil {
		return Result{}, err
	}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if rel == syncMetadataDir {
				return filepath.SkipDir
			}
			return nil
		}
		if ignoredByPatterns(rel, opts.IgnoreSuffixes) || ignoreRules.ignored(rel) {
			return nil
		}
		manifest, err := fileManifest(path, opts.BlockSize, hashBlocks)
		if err != nil {
			result.Inaccessible = append(result.Inaccessible, InaccessibleFile{RelativePath: rel, Error: err.Error()})
			return nil
		}
		result.Files = append(result.Files, File{RelativePath: rel, Manifest: manifest})
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	sort.Slice(result.Files, func(i, j int) bool { return result.Files[i].RelativePath < result.Files[j].RelativePath })
	sort.Slice(result.Inaccessible, func(i, j int) bool { return result.Inaccessible[i].RelativePath < result.Inaccessible[j].RelativePath })
	return result, nil
}

func fileManifest(path string, blockSize int, hashBlocks bool) (block.Manifest, error) {
	if hashBlocks {
		manifest, err := block.BuildManifest(path, blockSize)
		if err != nil {
			return block.Manifest{}, err
		}
		info, err := os.Stat(path)
		if err != nil {
			return block.Manifest{}, err
		}
		manifest.ModTimeUnixNano = info.ModTime().UnixNano()
		manifest.ChangeTimeUnixNano = changeTimeUnixNano(info)
		return manifest, nil
	}
	if blockSize <= 0 {
		return block.Manifest{}, os.ErrInvalid
	}
	info, err := os.Stat(path)
	if err != nil {
		return block.Manifest{}, err
	}
	return block.Manifest{
		Path:               path,
		Size:               info.Size(),
		BlockSize:          blockSize,
		HashState:          "unknown",
		ModTimeUnixNano:    info.ModTime().UnixNano(),
		ChangeTimeUnixNano: changeTimeUnixNano(info),
	}, nil
}

const syncMetadataDir = ".sync"

type ignoreRule struct {
	pattern       string
	include       bool
	anchored      bool
	directoryOnly bool
	caseFold      bool
}

type syncIgnoreRules []ignoreRule

func loadSyncIgnoreRules(root string) (syncIgnoreRules, error) {
	primary := filepath.Join(root, syncMetadataDir, "ignore")
	if _, err := os.Stat(primary); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return loadSyncIgnoreFile(root, primary, make(map[string]bool))
}

func loadSyncIgnoreFile(root, name string, seen map[string]bool) (syncIgnoreRules, error) {
	cleanName, err := filepath.Abs(name)
	if err != nil {
		return nil, err
	}
	if seen[cleanName] {
		return nil, nil
	}
	seen[cleanName] = true

	file, err := os.Open(cleanName)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var rules syncIgnoreRules
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if includeName, ok := parseIgnoreInclude(line); ok {
			included, err := loadSyncIgnoreFile(root, resolveIgnoreInclude(root, includeName), seen)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return nil, err
			}
			rules = append(rules, included...)
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		rule := ignoreRule{}
		if strings.HasPrefix(line, "(?i)") {
			rule.caseFold = true
			line = strings.TrimPrefix(line, "(?i)")
		}
		if strings.HasPrefix(line, "!") {
			rule.include = true
			line = strings.TrimSpace(strings.TrimPrefix(line, "!"))
		}
		if strings.HasPrefix(line, "(?i)") {
			rule.caseFold = true
			line = strings.TrimPrefix(line, "(?i)")
		}
		line = filepath.ToSlash(line)
		rule.anchored = strings.HasPrefix(line, "/")
		rule.directoryOnly = strings.HasSuffix(line, "/")
		line = strings.TrimPrefix(line, "/")
		line = strings.TrimSuffix(line, "/")
		if line == "" {
			continue
		}
		rule.pattern = line
		rules = append(rules, rule)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return rules, nil
}

func MissingInShareSyncIgnoreIncludes(root string) ([]string, error) {
	primary := filepath.Join(root, syncMetadataDir, "ignore")
	if _, err := os.Stat(primary); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return missingInShareSyncIgnoreIncludes(root, primary, make(map[string]bool))
}

func missingInShareSyncIgnoreIncludes(root, name string, seen map[string]bool) ([]string, error) {
	cleanName, err := filepath.Abs(name)
	if err != nil {
		return nil, err
	}
	if seen[cleanName] {
		return nil, nil
	}
	seen[cleanName] = true
	file, err := os.Open(cleanName)
	if err != nil {
		if os.IsNotExist(err) && pathInsideRoot(root, cleanName) {
			rel, relErr := filepath.Rel(root, cleanName)
			if relErr != nil {
				return nil, relErr
			}
			return []string{filepath.ToSlash(rel)}, nil
		}
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()
	var missing []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		includeName, ok := parseIgnoreInclude(line)
		if !ok {
			continue
		}
		found, err := missingInShareSyncIgnoreIncludes(root, resolveIgnoreInclude(root, includeName), seen)
		if err != nil {
			return nil, err
		}
		missing = append(missing, found...)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	sort.Strings(missing)
	return missing, nil
}

func pathInsideRoot(root, name string) bool {
	cleanRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	cleanName, err := filepath.Abs(name)
	if err != nil {
		return false
	}
	return cleanName == cleanRoot || strings.HasPrefix(cleanName, cleanRoot+string(os.PathSeparator))
}

func parseIgnoreInclude(line string) (string, bool) {
	const directive = "#include"
	if !strings.HasPrefix(line, directive) {
		return "", false
	}
	if len(line) > len(directive) {
		next := line[len(directive)]
		if next != ' ' && next != '\t' {
			return "", false
		}
	}
	name := strings.TrimSpace(strings.TrimPrefix(line, directive))
	return name, name != ""
}

func resolveIgnoreInclude(root, name string) string {
	if filepath.IsAbs(name) {
		return name
	}
	return filepath.Join(root, filepath.FromSlash(name))
}

type SyncIgnoreMatcher struct {
	rules syncIgnoreRules
}

func LoadSyncIgnoreMatcher(root string) (SyncIgnoreMatcher, error) {
	rules, err := loadSyncIgnoreRules(root)
	if err != nil {
		return SyncIgnoreMatcher{}, err
	}
	return SyncIgnoreMatcher{rules: rules}, nil
}

func (m SyncIgnoreMatcher) IsIgnored(rel string) bool {
	return m.rules.ignored(filepath.ToSlash(rel))
}

func IsSyncIgnored(root, rel string) (bool, error) {
	matcher, err := LoadSyncIgnoreMatcher(root)
	if err != nil {
		return false, err
	}
	return matcher.IsIgnored(rel), nil
}

func (rules syncIgnoreRules) ignored(rel string) bool {
	ignored := false
	for _, rule := range rules {
		if matchSyncIgnoreRule(rule, rel) {
			ignored = !rule.include
		}
	}
	return ignored
}

func ignoredByPatterns(rel string, patterns []string) bool {
	for _, pattern := range patterns {
		if matchIgnorePattern(filepath.ToSlash(pattern), rel) || strings.HasSuffix(rel, pattern) {
			return true
		}
	}
	return false
}

func matchIgnorePattern(pattern, rel string) bool {
	pattern = strings.TrimPrefix(pattern, "/")
	if strings.HasSuffix(pattern, "/") {
		return strings.HasPrefix(rel, strings.TrimSuffix(pattern, "/")+"/")
	}
	matched, err := path.Match(pattern, rel)
	if err == nil && matched {
		return true
	}
	if !strings.Contains(pattern, "/") {
		matched, err = path.Match(pattern, path.Base(rel))
		return err == nil && matched
	}
	return false
}

func matchSyncIgnoreRule(rule ignoreRule, rel string) bool {
	if rule.caseFold {
		rule.pattern = strings.ToLower(rule.pattern)
		rel = strings.ToLower(rel)
	}
	if rule.directoryOnly {
		return matchDirectoryRule(rule, rel)
	}
	return matchPathRule(rule, rel)
}

func matchDirectoryRule(rule ignoreRule, rel string) bool {
	prefix := strings.Trim(rule.pattern, "/")
	if prefix == "" {
		return false
	}
	if rule.anchored {
		return rel == prefix || strings.HasPrefix(rel, prefix+"/")
	}
	return rel == prefix || strings.HasPrefix(rel, prefix+"/") || strings.Contains(rel, "/"+prefix+"/")
}

func matchPathRule(rule ignoreRule, rel string) bool {
	pattern := rule.pattern
	if rule.anchored || strings.Contains(pattern, "/") {
		if matchGlobPath(pattern, rel) {
			return true
		}
		if rule.anchored {
			return false
		}
		for i := 0; i < len(rel); i++ {
			if rel[i] == '/' && matchGlobPath(pattern, rel[i+1:]) {
				return true
			}
		}
		return false
	}
	matched, err := path.Match(pattern, path.Base(rel))
	return err == nil && matched
}

func matchGlobPath(pattern, rel string) bool {
	matched, err := path.Match(pattern, rel)
	if err == nil && matched {
		return true
	}
	if !strings.Contains(pattern, "**") {
		return false
	}
	re, err := regexp.Compile("^" + globToRegexp(pattern) + "$")
	return err == nil && re.MatchString(rel)
}

func globToRegexp(pattern string) string {
	var b strings.Builder
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '\\':
			if i+1 < len(pattern) {
				i++
				b.WriteString(regexp.QuoteMeta(string(pattern[i])))
			} else {
				b.WriteString(regexp.QuoteMeta("\\"))
			}
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i++
				if i+1 < len(pattern) && pattern[i+1] == '/' {
					i++
					b.WriteString("(?:.*/)?")
				} else {
					b.WriteString(".*")
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '[':
			class, next, ok := globCharClass(pattern, i)
			if !ok {
				b.WriteString(regexp.QuoteMeta("["))
				continue
			}
			b.WriteString(class)
			i = next
		default:
			b.WriteString(regexp.QuoteMeta(string(pattern[i])))
		}
	}
	return b.String()
}

func globCharClass(pattern string, start int) (string, int, bool) {
	end := start + 1
	if end < len(pattern) && (pattern[end] == '!' || pattern[end] == '^') {
		end++
	}
	if end < len(pattern) && pattern[end] == ']' {
		end++
	}
	for end < len(pattern) {
		if pattern[end] == '\\' {
			end += 2
			continue
		}
		if pattern[end] == ']' {
			return regexpCharClass(pattern[start+1 : end]), end, true
		}
		end++
	}
	return "", start, false
}

func regexpCharClass(class string) string {
	var b strings.Builder
	b.WriteByte('[')
	if strings.HasPrefix(class, "!") {
		b.WriteByte('^')
		class = strings.TrimPrefix(class, "!")
	}
	for i := 0; i < len(class); i++ {
		if class[i] == '\\' && i+1 < len(class) {
			i++
			b.WriteString(regexp.QuoteMeta(string(class[i])))
			continue
		}
		b.WriteByte(class[i])
	}
	b.WriteByte(']')
	return b.String()
}
