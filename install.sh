#!/bin/sh

set -eu

repo="haowang02/agent-session-cleaner"
binary="agent-session-cleaner"
alias_name="asc"

say() {
	printf '%s\n' "$*"
}

fail() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

command -v curl >/dev/null 2>&1 || fail "curl is required"
command -v tar >/dev/null 2>&1 || fail "tar is required"

case "$(uname -s)" in
	Darwin) platform="macos" ;;
	Linux) platform="linux" ;;
	*) fail "unsupported operating system: $(uname -s)" ;;
esac

case "$(uname -m)" in
	arm64 | aarch64) arch="arm64" ;;
	x86_64 | amd64) arch="x86_64" ;;
	*) fail "unsupported architecture: $(uname -m)" ;;
esac

version=${ASC_VERSION:-latest}
case "$version" in
	latest) release_path="latest/download" ;;
	*[!A-Za-z0-9._-]*) fail "invalid ASC_VERSION: $version" ;;
	v*) release_path="download/$version" ;;
	*) release_path="download/v$version" ;;
esac

if [ -n "${ASC_INSTALL_DIR:-}" ]; then
	install_dir=$ASC_INSTALL_DIR
else
	[ -n "${HOME:-}" ] || fail "HOME is not set; set ASC_INSTALL_DIR explicitly"
	install_dir=${XDG_BIN_HOME:-"$HOME/.local/bin"}
fi
case "$install_dir" in
	/*) ;;
	*) fail "the install directory must be an absolute path: $install_dir" ;;
esac

asset="$binary-$platform-$arch.tar.gz"
base_url="https://github.com/$repo/releases/$release_path"
tmp_dir=$(mktemp -d "${TMPDIR:-/tmp}/agent-session-cleaner.XXXXXX") ||
	fail "could not create a temporary directory"
staged_file=""
cleanup() {
	[ -z "$staged_file" ] || rm -f "$staged_file"
	rm -rf "$tmp_dir"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM

archive="$tmp_dir/$asset"
checksums="$tmp_dir/checksums.txt"
say "Downloading $asset..."
curl -fLsS --retry 3 -o "$archive" "$base_url/$asset" ||
	fail "could not download $asset"
curl -fLsS --retry 3 -o "$checksums" "$base_url/checksums.txt" ||
	fail "could not download checksums.txt"

expected=$(awk -v file="$asset" '$2 == file || $2 == ("*" file) { print $1; exit }' "$checksums")
printf '%s\n' "$expected" | grep -Eq '^[0-9A-Fa-f]{64}$' ||
	fail "checksums.txt has no valid entry for $asset"
if command -v sha256sum >/dev/null 2>&1; then
	actual=$(sha256sum "$archive" | awk '{ print $1 }')
elif command -v shasum >/dev/null 2>&1; then
	actual=$(shasum -a 256 "$archive" | awk '{ print $1 }')
else
	fail "sha256sum or shasum is required to verify the download"
fi
[ "$actual" = "$expected" ] || fail "checksum verification failed for $asset"

contents=$(tar -tzf "$archive") || fail "could not inspect $asset"
case "$contents" in
	"$binary" | "./$binary") ;;
	*) fail "$asset does not contain exactly one $binary executable" ;;
esac

mkdir -p "$tmp_dir/extract"
tar -xzf "$archive" -C "$tmp_dir/extract" || fail "could not extract $asset"
executable="$tmp_dir/extract/$binary"
[ -f "$executable" ] || fail "$asset does not contain $binary"

mkdir -p "$install_dir" || fail "could not create $install_dir"
staged_file="$install_dir/.$binary.install.$$"
install -m 755 "$executable" "$staged_file" || fail "could not write to $install_dir"
mv -f "$staged_file" "$install_dir/$binary" || fail "could not install $binary"
staged_file=""

alias_path="$install_dir/$alias_name"
alias_created=false
run_command=$binary
existing_alias=$(command -v "$alias_name" 2>/dev/null || true)
link_target=""
[ ! -L "$alias_path" ] || link_target=$(readlink "$alias_path" 2>/dev/null || true)
if [ -n "$existing_alias" ]; then
	if [ "$existing_alias" = "$alias_path" ] &&
		{ [ "$link_target" = "$binary" ] || [ "$link_target" = "$install_dir/$binary" ]; }; then
		say "Keeping $alias_path -> $link_target"
		run_command=$alias_name
	else
		say "Leaving existing $alias_name command unchanged: $existing_alias"
	fi
elif [ "$link_target" = "$binary" ] || [ "$link_target" = "$install_dir/$binary" ]; then
	say "Keeping $alias_path -> $link_target"
	run_command=$alias_name
elif [ -e "$alias_path" ] || [ -L "$alias_path" ]; then
	say "Not replacing existing $alias_path"
else
	ln -s "$binary" "$alias_path" || fail "could not create $alias_path"
	alias_created=true
	run_command=$alias_name
fi

say "Installed $binary to $install_dir/$binary"
[ "$alias_created" = false ] || say "Created $alias_path -> $binary"
case ":${PATH:-}:" in
	*":$install_dir:"*)
		say "Run '$run_command' to get started."
		;;
	*)
		say "Add the install directory to PATH, then run '$run_command':"
		say "  export PATH=\"$install_dir:\$PATH\""
		;;
esac
