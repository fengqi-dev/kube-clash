//go:build darwin

package singbox

import (
	"embed"
	"os"
	"path/filepath"
	"strings"
)

const (
	searchSetupScriptName   = "setup-search-domains.sh"
	searchRestoreScriptName = "restore-search-domains.sh"
	searchDomainsListName   = "search-domains.list"
)

//go:embed setup-search-domains.sh restore-search-domains.sh
var searchDomainScripts embed.FS

func writeSearchDomainScripts(workDir string, search []string) error {
	if err := os.WriteFile(
		filepath.Join(workDir, searchDomainsListName),
		[]byte(strings.Join(search, "\n")+"\n"),
		0o600,
	); err != nil {
		return err
	}
	for _, name := range []string{searchSetupScriptName, searchRestoreScriptName} {
		data, err := searchDomainScripts.ReadFile(name)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(workDir, name), data, 0o700); err != nil {
			return err
		}
	}
	return nil
}
