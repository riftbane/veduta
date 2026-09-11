#!/bin/sh
# Acceptance §15.1 on a fresh Ubuntu 24.04 (docker ubuntu:24.04 + the packages a server
# image already has): install.sh → veduta init demo → cd demo → veduta test, as a normal
# user, timed, with every non-GitHub host blocked after the installation.
set -eu
apt-get update -qq >/dev/null && apt-get install -y -qq curl git ca-certificates sudo >/dev/null
useradd -m -s /bin/bash dev
cat /etc/os-release | grep PRETTY_NAME

start=$(date +%s)
su - dev -c 'curl -fsSL https://raw.githubusercontent.com/riftbane/veduta/main/install.sh | sh'
installed=$(date +%s)

# After installation only GitHub may be reached.
for h in proxy.golang.org sum.golang.org go.dev golang.org dl.google.com storage.googleapis.com goproxy.io; do
	echo "0.0.0.0 $h" >>/etc/hosts
done

su - dev -c 'export PATH=$HOME/.local/bin:$HOME/.local/go/bin:$PATH
set -e
veduta version
veduta init demo
cd demo
veduta test'
done_=$(date +%s)
echo "TIMING install=$((installed - start))s init+test=$((done_ - installed))s total=$((done_ - start))s"
