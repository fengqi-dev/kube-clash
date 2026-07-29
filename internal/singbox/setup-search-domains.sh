#!/bin/sh
# Prefer active network services over the default-route iface. When Clash or
# another TUN owns the default route, iface is utun and has no networksetup
# service name — the old script exited without setting search domains.
set -e
dir=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
backup="$dir/search-domains.bak"
domains_file="$dir/search-domains.list"

services=""
/usr/sbin/networksetup -listallnetworkservices 2>/dev/null | /usr/bin/tail -n +2 | while IFS= read -r svc; do
  [ -z "$svc" ] && continue
  case "$svc" in \*) continue ;; esac
  if /usr/sbin/networksetup -getinfo "$svc" 2>/dev/null | /usr/bin/grep -q '^IP address:'; then
    echo "$svc"
  fi
done > "$backup.services"
if [ ! -s "$backup.services" ]; then
  iface=$(/sbin/route -n get default 2>/dev/null | /usr/bin/awk '/interface:/{print $2; exit}')
  service=$(/usr/sbin/networksetup -listallhardwareports 2>/dev/null | /usr/bin/awk -v dev="$iface" 'BEGIN{port=""} /^Hardware Port: /{port=substr($0,16)} /^Device: /{if($2==dev){print port; exit}}')
  if [ -n "$service" ]; then echo "$service" > "$backup.services"; fi
fi
if [ ! -s "$backup.services" ]; then exit 0; fi
: > "$backup"
while IFS= read -r service; do
  [ -z "$service" ] && continue
  current=$(/usr/sbin/networksetup -getsearchdomains "$service" 2>/dev/null || true)
  /usr/bin/printf '%s\t%s\n' "$service" "$(/usr/bin/printf '%s' "$current" | /usr/bin/tr '\n' ' ')" >> "$backup"
  set --
  if [ -f "$domains_file" ]; then
    while IFS= read -r domain || [ -n "$domain" ]; do
      [ -n "$domain" ] && set -- "$@" "$domain"
    done < "$domains_file"
  fi
  if [ -n "$current" ] && ! /usr/bin/printf '%s' "$current" | /usr/bin/grep -qi "aren.t any Search Domains"; then
    old=$(/usr/bin/printf '%s\n' "$current" | /usr/bin/tr '\t ' '\n\n')
    for d in $old; do
      [ -z "$d" ] && continue
      skip=0
      if [ -f "$domains_file" ]; then
        while IFS= read -r want || [ -n "$want" ]; do
          [ "$d" = "$want" ] && skip=1 && break
        done < "$domains_file"
      fi
      if [ "$skip" -eq 0 ]; then set -- "$@" "$d"; fi
    done
  fi
  /usr/sbin/networksetup -setsearchdomains "$service" "$@" >/dev/null 2>&1 || true
done < "$backup.services"
/bin/rm -f "$backup.services"
/usr/bin/dscacheutil -flushcache >/dev/null 2>&1 || true
/usr/bin/killall -HUP mDNSResponder >/dev/null 2>&1 || true
