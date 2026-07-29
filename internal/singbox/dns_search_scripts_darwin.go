//go:build darwin

package singbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	searchSetupScriptName   = "setup-search-domains.sh"
	searchRestoreScriptName = "restore-search-domains.sh"
	searchBackupFileName    = "search-domains.bak"
)

func writeSearchDomainScripts(workDir string, search []string) error {
	if err := writeMacSearchSetupScript(workDir, search); err != nil {
		return err
	}
	return writeMacSearchRestoreScript(workDir)
}

func writeMacSearchSetupScript(workDir string, search []string) error {
	var lines []string
	lines = append(lines, "#!/bin/sh", "set -e", "")
	lines = append(lines, `backup="`+filepath.Join(workDir, searchBackupFileName)+`"`)
	lines = append(lines, `iface=$(/sbin/route -n get default 2>/dev/null | /usr/bin/awk '/interface:/{print $2; exit}')`)
	lines = append(lines, `service=$(/usr/sbin/networksetup -listallhardwareports 2>/dev/null | /usr/bin/awk -v dev="$iface" 'BEGIN{port=""} /^Hardware Port: /{port=substr($0,16)} /^Device: /{if($2==dev){print port; exit}}')`)
	lines = append(lines, `if [ -z "$service" ]; then exit 0; fi`)
	lines = append(lines, `current=$(/usr/sbin/networksetup -getsearchdomains "$service" 2>/dev/null || true)`)
	lines = append(lines, `/usr/bin/printf '%s\n%s\n' "$service" "$current" > "$backup"`)
	lines = append(lines, `set --`)
	for _, domain := range search {
		lines = append(lines, fmt.Sprintf("set -- \"$@\" %s", shellQuote(domain)))
	}
	lines = append(lines, `if [ -n "$current" ] && ! /usr/bin/printf '%s' "$current" | /usr/bin/grep -qi "aren.t any Search Domains"; then`)
	lines = append(lines, `  old=$(/usr/bin/printf '%s\n' "$current" | /usr/bin/tr '\t ' '\n\n')`)
	lines = append(lines, `  for d in $old; do`)
	lines = append(lines, `    [ -z "$d" ] && continue`)
	lines = append(lines, `    skip=0`)
	for _, domain := range search {
		lines = append(lines, fmt.Sprintf("    if [ \"$d\" = %s ]; then skip=1; fi", shellQuote(domain)))
	}
	lines = append(lines, `    if [ "$skip" -eq 0 ]; then set -- "$@" "$d"; fi`)
	lines = append(lines, `  done`)
	lines = append(lines, `fi`)
	lines = append(lines, `/usr/sbin/networksetup -setsearchdomains "$service" "$@" >/dev/null 2>&1 || true`)
	lines = append(lines, `/usr/bin/dscacheutil -flushcache >/dev/null 2>&1 || true`)
	lines = append(lines, `/usr/bin/killall -HUP mDNSResponder >/dev/null 2>&1 || true`)
	return os.WriteFile(
		filepath.Join(workDir, searchSetupScriptName),
		[]byte(strings.Join(lines, "\n")+"\n"),
		0o700,
	)
}

func writeMacSearchRestoreScript(workDir string) error {
	content := `#!/bin/sh
set -e
backup="` + filepath.Join(workDir, searchBackupFileName) + `"
if [ ! -f "$backup" ]; then exit 0; fi
service=$(/usr/bin/sed -n '1p' "$backup")
rest=$(/usr/bin/sed -n '2,$p' "$backup")
/bin/rm -f "$backup"
if [ -z "$service" ]; then exit 0; fi
if [ -z "$rest" ] || /usr/bin/printf '%s' "$rest" | /usr/bin/grep -qi "aren.t any Search Domains"; then
  /usr/sbin/networksetup -setsearchdomains "$service" Empty >/dev/null 2>&1 || true
  exit 0
fi
set --
old=$(/usr/bin/printf '%s\n' "$rest" | /usr/bin/tr '\t ' '\n\n')
for d in $old; do
  [ -n "$d" ] && set -- "$@" "$d"
done
if [ "$#" -eq 0 ]; then
  /usr/sbin/networksetup -setsearchdomains "$service" Empty >/dev/null 2>&1 || true
else
  /usr/sbin/networksetup -setsearchdomains "$service" "$@" >/dev/null 2>&1 || true
fi
/usr/bin/dscacheutil -flushcache >/dev/null 2>&1 || true
/usr/bin/killall -HUP mDNSResponder >/dev/null 2>&1 || true
`
	return os.WriteFile(filepath.Join(workDir, searchRestoreScriptName), []byte(content), 0o700)
}
