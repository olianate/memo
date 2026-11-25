// Command release keeps the application version in one place.
//
// The release tag is the source of truth during a release build:
//
//	go run ./tools/release set 1.4.0
//	go run ./tools/release check
//	go run ./tools/release check 1.4.0 -notes
//	go run ./tools/release print
//
// The version is written to build/config.yml, the Windows version resource,
// every Info.plist and the Go, npm and npm-lock manifests. Everything is a
// line-oriented replacement so comments, ordering and indentation survive.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var versionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)

// A field is one version location. The regexp has three groups: the text
// before the version, the version itself and the text after it. unique is
// false for package-lock.json, where dependency entries share the same
// indentation; only the first match (the root package) is used.
type field struct {
	file   string
	name   string
	re     *regexp.Regexp
	unique bool
}

var fields = []field{
	{
		file:   "build/config.yml",
		name:   "info.version",
		re:     regexp.MustCompile(`(?m)^(  version:\s*")([^"]*)(")`),
		unique: true,
	},
	{
		file:   "build/windows/info.json",
		name:   "file_version",
		re:     regexp.MustCompile(`("file_version"\s*:\s*")([^"]*)(")`),
		unique: true,
	},
	{
		file:   "build/windows/info.json",
		name:   "product_version",
		re:     regexp.MustCompile(`("product_version"\s*:\s*")([^"]*)(")`),
		unique: true,
	},
	{
		file:   "build/windows/info.json",
		name:   "FileVersion",
		re:     regexp.MustCompile(`("FileVersion"\s*:\s*")([^"]*)(")`),
		unique: true,
	},
	{
		file:   "build/windows/info.json",
		name:   "ProductVersion",
		re:     regexp.MustCompile(`("ProductVersion"\s*:\s*")([^"]*)(")`),
		unique: true,
	},
	{
		file:   "build/darwin/Info.plist",
		name:   "CFBundleShortVersionString",
		re:     regexp.MustCompile(`(?s)(<key>CFBundleShortVersionString</key>\s*<string>)([^<]*)(</string>)`),
		unique: true,
	},
	{
		file:   "build/darwin/Info.plist",
		name:   "CFBundleVersion",
		re:     regexp.MustCompile(`(?s)(<key>CFBundleVersion</key>\s*<string>)([^<]*)(</string>)`),
		unique: true,
	},
	{
		file:   "build/darwin/Info.dev.plist",
		name:   "CFBundleShortVersionString",
		re:     regexp.MustCompile(`(?s)(<key>CFBundleShortVersionString</key>\s*<string>)([^<]*)(</string>)`),
		unique: true,
	},
	{
		file:   "build/darwin/Info.dev.plist",
		name:   "CFBundleVersion",
		re:     regexp.MustCompile(`(?s)(<key>CFBundleVersion</key>\s*<string>)([^<]*)(</string>)`),
		unique: true,
	},
	{
		file:   "build/ios/Info.plist",
		name:   "CFBundleShortVersionString",
		re:     regexp.MustCompile(`(?s)(<key>CFBundleShortVersionString</key>\s*<string>)([^<]*)(</string>)`),
		unique: true,
	},
	{
		file:   "build/ios/Info.plist",
		name:   "CFBundleVersion",
		re:     regexp.MustCompile(`(?s)(<key>CFBundleVersion</key>\s*<string>)([^<]*)(</string>)`),
		unique: true,
	},
	{
		file:   "internal/services/version.go",
		name:   "Version",
		re:     regexp.MustCompile(`(?m)^(var Version = ")([^"]*)(")`),
		unique: true,
	},
	{
		file:   "frontend/package.json",
		name:   "version",
		re:     regexp.MustCompile(`(?m)^(  "version":\s*")([^"]*)(")`),
		unique: true,
	},
	{
		file:   "frontend/package-lock.json",
		name:   "version",
		re:     regexp.MustCompile(`(?m)^(  "version":\s*")([^"]*)(")`),
		unique: true,
	},
	{
		// npm lockfiles also use six spaces for dependency versions. The root
		// package entry is the first one, so only that match is replaced.
		file:   "frontend/package-lock.json",
		name:   `packages[""].version`,
		re:     regexp.MustCompile(`(?m)^(      "version":\s*")([^"]*)(")`),
		unique: false,
	},
}

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}

	var err error
	switch args[0] {
	case "set":
		if len(args) != 2 {
			usage()
			os.Exit(2)
		}
		err = set(".", args[1])
	case "check":
		var version string
		notes := false
		for _, arg := range args[1:] {
			switch {
			case arg == "-notes":
				notes = true
			case strings.HasPrefix(arg, "-"):
				err = fmt.Errorf("unknown flag %s", arg)
			case version == "":
				version = arg
			default:
				err = fmt.Errorf("check accepts one version, got %q and %q", version, arg)
			}
			if err != nil {
				break
			}
		}
		if err == nil {
			err = check(".", version, notes)
		}
	case "print":
		if len(args) != 1 {
			usage()
			os.Exit(2)
		}
		err = printVersion(".")
	default:
		usage()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "release:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  release set <version>
  release check [version] [-notes]
  release print`)
}

func normalizeVersion(version string) (string, error) {
	version = strings.TrimSpace(version)
	if len(version) > 0 && (version[0] == 'v' || version[0] == 'V') {
		version = version[1:]
	}
	if version == "" {
		return "", errors.New("version is empty")
	}
	if !versionPattern.MatchString(version) {
		return "", fmt.Errorf("invalid version %q: want X.Y.Z or vX.Y.Z", version)
	}
	return version, nil
}

func readFile(root, name string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func versionAt(content string, f field) (string, error) {
	loc := f.re.FindStringSubmatchIndex(content)
	if loc == nil {
		return "", fmt.Errorf("%s: %s not found", f.file, f.name)
	}
	return content[loc[4]:loc[5]], nil
}

func replaceVersion(content string, f field, version string) (string, error) {
	if f.unique && len(f.re.FindAllStringSubmatchIndex(content, -1)) != 1 {
		return "", fmt.Errorf("%s: expected exactly one %s", f.file, f.name)
	}
	loc := f.re.FindStringSubmatchIndex(content)
	if loc == nil {
		return "", fmt.Errorf("%s: %s not found", f.file, f.name)
	}
	return content[:loc[4]] + version + content[loc[5]:], nil
}

// set rewrites every version location. It reads all fields before writing any
// file so a malformed tree fails without leaving a partial bump behind.
func set(root, version string) error {
	version, err := normalizeVersion(version)
	if err != nil {
		return err
	}

	files := make([]string, 0, len(fields))
	seen := make(map[string]bool, len(fields))
	for _, f := range fields {
		if !seen[f.file] {
			seen[f.file] = true
			files = append(files, f.file)
		}
	}
	sort.Strings(files)

	updated := make(map[string]string, len(files))
	for _, file := range files {
		content, err := readFile(root, file)
		if err != nil {
			return err
		}
		for _, f := range fields {
			if f.file != file {
				continue
			}
			content, err = replaceVersion(content, f, version)
			if err != nil {
				return err
			}
		}
		updated[file] = content
	}

	changed := 0
	for _, file := range files {
		path := filepath.Join(root, filepath.FromSlash(file))
		current, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if string(current) == updated[file] {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(updated[file]), info.Mode()); err != nil {
			return err
		}
		fmt.Printf("updated %s\n", file)
		changed++
	}
	if changed == 0 {
		fmt.Printf("%s is already at %s\n", strings.Join(files, ", "), version)
	}
	return nil
}

// check verifies every location against version. An empty version means the
// version in build/config.yml, which keeps CI and local pre-commit checks
// simple. With notes it also requires the matching release-notes file.
func check(root, version string, notes bool) error {
	if version == "" {
		content, err := readFile(root, fields[0].file)
		if err != nil {
			return err
		}
		version, err = versionAt(content, fields[0])
		if err != nil {
			return err
		}
	}
	version, err := normalizeVersion(version)
	if err != nil {
		return err
	}

	var problems []error
	for _, f := range fields {
		content, err := readFile(root, f.file)
		if err != nil {
			problems = append(problems, err)
			continue
		}
		got, err := versionAt(content, f)
		if err != nil {
			problems = append(problems, err)
			continue
		}
		if got != version {
			problems = append(problems, fmt.Errorf("%s: %s is %q, want %q", f.file, f.name, got, version))
		}
	}

	if notes {
		path := filepath.Join(root, "docs", "releases", "v"+version+".md")
		if _, err := os.Stat(path); err != nil {
			problems = append(problems, fmt.Errorf("release notes missing: %s", filepath.ToSlash(path)))
		}
	}
	if len(problems) > 0 {
		return errors.Join(problems...)
	}

	fmt.Printf("version %s matches all %d locations", version, len(fields))
	if notes {
		fmt.Printf(" and docs/releases/v%s.md exists", version)
	}
	fmt.Println()
	return nil
}

func printVersion(root string) error {
	content, err := readFile(root, fields[0].file)
	if err != nil {
		return err
	}
	version, err := versionAt(content, fields[0])
	if err != nil {
		return err
	}
	fmt.Println(version)
	return nil
}
