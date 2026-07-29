#!/bin/sh
set -e
dir=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
backup="$dir/search-domains.bak"
if [ ! -f "$backup" ]; then exit 0; fi
while IFS= read -r line; do
  [ -z "$line" ] && continue
  service=$(/usr/bin/printf '%s' "$line" | /usr/bin/cut -f1)
  rest=$(/usr/bin/printf '%s' "$line" | /usr/bin/cut -f2-)
  [ -z "$service" ] && continue
  if [ -z "$rest" ] || /usr/bin/printf '%s' "$rest" | /usr/bin/grep -qi "aren.t any Search Domains"; then
    /usr/sbin/networksetup -setsearchdomains "$service" Empty >/dev/null 2>&1 || true
    continue
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
done < "$backup"
/bin/rm -f "$backup"
/usr/bin/dscacheutil -flushcache >/dev/null 2>&1 || true
/usr/bin/killall -HUP mDNSResponder >/dev/null 2>&1 || true
