#!/bin/sh
# Acceptance §15.1 on a fresh Ubuntu 24.04 (docker ubuntu:24.04 + the packages a server
# image already has): install.sh → veduta init demo → cd demo → veduta test, as a normal
# user, timed, with every non-GitHub host blocked after the installation.
#
# Usage: sh fresh_vps.sh [vX.Y.Z | stable | beta]
#   vX.Y.Z   install exactly that release (a candidate such as v1.0.0-rc.1 included) and
#            check that the tool and the project it creates are that version
#   stable   the newest stable release (the default)
#   beta     the newest release or candidate
# For example: docker run --rm -v "$PWD/scripts/acceptance:/a" ubuntu:24.04 sh /a/fresh_vps.sh v1.0.0-rc.1
#
# `su -` starts a clean environment, so the choice reaches install.sh as a flag, never
# through VEDUTA_VERSION or VEDUTA_CHANNEL.
set -eu
want="${1:-stable}"
case "$want" in
stable | beta) flags="--channel $want" ver="" ;;
v[0-9]*) flags="--version $want" ver="$want" ;;
*) echo "fresh_vps.sh: want vX.Y.Z, stable or beta, got $want" >&2; exit 2 ;;
esac

apt-get update -qq >/dev/null && apt-get install -y -qq curl git ca-certificates sudo >/dev/null
useradd -m -s /bin/bash dev
cat /etc/os-release | grep PRETTY_NAME

start=$(date +%s)
su - dev -c "curl -fsSL https://raw.githubusercontent.com/riftbane/veduta/main/install.sh | sh -s -- $flags"
installed=$(date +%s)

# After installation only GitHub may be reached.
for h in proxy.golang.org sum.golang.org go.dev golang.org dl.google.com storage.googleapis.com goproxy.io; do
	echo "0.0.0.0 $h" >>/etc/hosts
done

su - dev -c "export PATH=\$HOME/.local/bin:\$HOME/.local/go/bin:\$PATH
set -e
ver='$ver'
veduta version
if [ -n \"\$ver\" ]; then
	# A release that is not the one asked for would pass while testing another version.
	veduta version | grep -q \"^veduta \$ver \" || { echo \"installed tool is not \$ver\" >&2; exit 1; }
fi
veduta init demo
cd demo
if [ -n \"\$ver\" ]; then
	grep -q \"\\\"engine\\\": \\\"\$ver\\\"\" veduta.json || { echo \"demo/veduta.json does not pin engine \$ver\" >&2; exit 1; }
fi
veduta test"
done_=$(date +%s)
echo "TIMING install=$((installed - start))s init+test=$((done_ - installed))s total=$((done_ - start))s"
