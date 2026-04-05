package ingest

import (
	"path/filepath"
	"strings"
)

func DefaultConfig(repoRoot string) Config {
	root := filepath.Clean(repoRoot)
	return Config{
		RepoRoot:      root,
		AcceptedRoots: []string{filepath.Join(root, "fixtures"), filepath.Join(root, "contracts")},
		ManualRoots:   []string{filepath.Join(root, "review")},
		RejectedRoots: []string{filepath.Join(root, "private")},
		ParserVersion: DefaultParserVersion,
		RunTimestamp:  "1970-01-01T00:00:00Z",
	}
}

func (cfg Config) parserVersion() string {
	if cfg.ParserVersion == "" {
		return DefaultParserVersion
	}
	return cfg.ParserVersion
}

func (cfg Config) runTimestamp() string {
	if cfg.RunTimestamp == "" {
		return "1970-01-01T00:00:00Z"
	}
	return cfg.RunTimestamp
}

func (cfg Config) ClassifyPath(path string) RootDecision {
	clean := filepath.Clean(path)
	base := strings.ToLower(filepath.Base(clean))
	if strings.HasPrefix(base, ".env") || strings.Contains(base, "credential") {
		return RootDecision{State: RootRejected, Reason: "sensitive_file_excluded"}
	}
	for _, rejected := range cfg.RejectedRoots {
		if hasPathPrefix(clean, rejected) {
			return RootDecision{State: RootRejected, Reason: "rejected_root"}
		}
	}
	for _, manual := range cfg.ManualRoots {
		if hasPathPrefix(clean, manual) {
			return RootDecision{State: RootManualOnly, Reason: "manual_only_source"}
		}
	}
	for _, accepted := range cfg.AcceptedRoots {
		if hasPathPrefix(clean, accepted) {
			return RootDecision{State: RootAccepted}
		}
	}
	return RootDecision{State: RootRejected, Reason: "root_not_in_accepted_registry"}
}

func hasPathPrefix(path string, root string) bool {
	cleanPath := filepath.Clean(path)
	cleanRoot := filepath.Clean(root)
	if cleanPath == cleanRoot {
		return true
	}
	rel, err := filepath.Rel(cleanRoot, cleanPath)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
