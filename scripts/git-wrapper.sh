#!/usr/bin/env bash
# git-wrapper.sh: intercepts dangerous git commands in parallel-agent environments.
# Usage: alias git=this-script, or set as GIT_EXEC_PATH or use via agent config.
#
# Session tracking:
#   Each agent session gets a manifest under .git/staged-by/<SESSION_ID>.
#   git add records files there; git commit checks that only this session's
#   files are staged. This prevents accidentally committing other agents' work.
#
# Blocks:
#   - git commit --amend (sets sentinel for pre-commit hook to detect)
#   - git add . / git add -A / git add --all (forces explicit file paths)
#   - git commit when staged files include files not added by this session
#   - git reset --hard (destroys other agents' work)
#   - git stash (hides other agents' work)
#
# Bypass: GIT_UNSAFE=1 skips all checks (for human operators).

set -euo pipefail

if [ "${GIT_UNSAFE:-0}" = "1" ]; then
	command git "$@"
	exit $?
fi

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || true)"
SESSION_DIR="$REPO_ROOT/.git/staged-by"
SESSION_ID="${AGENT_SESSION_ID:-$$-$(date +%s)}"
SESSION_MANIFEST="$SESSION_DIR/$SESSION_ID"

_ensure_session_dir() {
	mkdir -p "$SESSION_DIR"
}

# Record files that this session has staged.
# Args: list of file paths (as passed to git add, after flag removal)
_record_staged() {
	_ensure_session_dir
	for f in "$@"; do
		# Normalize: make relative to repo root, strip leading ./
		rel="$(cd "$REPO_ROOT" && realpath --relative-to=. "$f" 2>/dev/null || echo "$f")"
		echo "$rel" >> "$SESSION_MANIFEST"
	done
	# Deduplicate and sort
	if [ -f "$SESSION_MANIFEST" ]; then
		sort -u -o "$SESSION_MANIFEST" "$SESSION_MANIFEST"
	fi
}

# Get all files this session has staged (from manifest).
_session_files() {
	if [ -f "$SESSION_MANIFEST" ]; then
		cat "$SESSION_MANIFEST"
	fi
}

# Get currently staged files from git index.
_staged_files() {
	command git diff --cached --name-only 2>/dev/null || true
}

# On successful commit, clean up this session's manifest.
_cleanup_session() {
	rm -f "$SESSION_MANIFEST"
}

case "${1:-}" in
commit)
	shift
	# Check for --amend
	for arg in "$@"; do
		if [ "$arg" = "--amend" ]; then
			echo "git-wrapper: BLOCKED 'git commit --amend' (parallel-agent safety)." >&2
			echo "  Amending can overwrite other agents' commits." >&2
			echo "  Set GIT_UNSAFE=1 if you really need this." >&2
			exit 1
		fi
	done

	# Check that all staged files belong to this session
	_session="$(_session_files)"
	_staged="$(_staged_files)"

	if [ -z "$_staged" ]; then
		echo "git-wrapper: nothing staged, nothing to commit." >&2
		exit 1
	fi

	if [ -n "$_session" ]; then
		# Find staged files not in this session's manifest
		_leaked="$(comm -23 <(echo "$_staged" | sort) <(echo "$_session" | sort))"
		if [ -n "$_leaked" ]; then
			echo "git-wrapper: BLOCKED — staged files not added by this session:" >&2
			echo "$_leaked" | sed 's/^/  /' >&2
			echo "" >&2
			echo "  These files were staged by another agent or a prior operation." >&2
			echo "  Only commit files you explicitly 'git add'-ed in this session." >&2
			echo "" >&2
			echo "  To fix: 'git restore --staged <file>' for each leaked file." >&2
			echo "  Or set GIT_UNSAFE=1 to bypass (human operators only)." >&2
			exit 1
		fi
	else
		# No manifest at all — this session never ran git add through the wrapper
		echo "git-wrapper: BLOCKED — no session staging manifest found." >&2
		echo "  This session has not added any files via 'git add'." >&2
		echo "  All currently staged files are from another source." >&2
		echo "  Set GIT_UNSAFE=1 to bypass (human operators only)." >&2
		exit 1
	fi

	command git commit "$@"
	_rc=$?
	if [ $_rc -eq 0 ]; then
		_cleanup_session
	fi
	exit $_rc
	;;
add)
	shift
	if [ $# -eq 0 ]; then
		echo "git-wrapper: BLOCKED bare 'git add' (no files specified)." >&2
		echo "  Specify exact files: git add <file1> <file2>" >&2
		exit 1
	fi
	# Separate flags from file paths, and check for broad patterns
	_files=()
	for arg in "$@"; do
		case "$arg" in
			.|-A|--all)
				echo "git-wrapper: BLOCKED 'git add $arg' — broad staging is not allowed." >&2
				echo "  Other agents may have uncommitted changes. Specify exact files." >&2
				exit 1
				;;
			-*)
				# Pass flags through (e.g. -p, -f, --force)
				_files+=("$arg")
				;;
			*)
				_files+=("$arg")
				;;
		esac
	done
	command git add "${_files[@]}"
	_rc=$?
	if [ $_rc -eq 0 ]; then
		# Record only the non-flag args
		_paths=()
		_skip_next=false
		for arg in "$@"; do
			if [ "$_skip_next" = true ]; then
				_skip_next=false
				continue
			fi
			case "$arg" in
				-p|--patch|-e|--edit)
					# Interactive modes: we can't know exactly what was staged
					# Record nothing — user is on their own for commit safety
					;;
				-u|--update)
					# --update stages tracked modified files — record what got staged
					_newly_staged="$(command git diff --cached --name-only)"
					_record_staged $_newly_staged
					;;
				-*)
					# Other flags: skip
					;;
				*)
					_record_staged "$arg"
					;;
			esac
		done
	fi
	exit $_rc
	;;
restore)
	# Allow 'git restore --staged' (unstage) — this is the escape hatch
	# Also clean up session manifest for any files being unstaged
	shift
	for arg in "$@"; do
		if [ "$arg" = "--staged" ] || [ "$arg" = "-S" ]; then
			# Remove unstaged files from our session manifest
			_staged_args=true
			break
		fi
	done
	command git restore "$@"
	# If unstaging, remove those files from our manifest
	if [ "${_staged_args:-false}" = true ] && [ -f "$SESSION_MANIFEST" ]; then
		_ensure_session_dir
		_unstaged_paths=()
		_next_is_path=false
		for arg in "$@"; do
			case "$arg" in
				--staged|-S|-W|--worktree)
					;;
				-*)
					;;
				*)
					_rel="$(cd "$REPO_ROOT" && realpath --relative-to=. "$arg" 2>/dev/null || echo "$arg")"
					_unstaged_paths+=("$_rel")
					;;
			esac
		done
		if [ ${#_unstaged_paths[@]} -gt 0 ]; then
			# Remove from manifest using grep -v
			_tmp="$SESSION_MANIFEST.tmp"
			cp "$SESSION_MANIFEST" "$_tmp"
			for p in "${_unstaged_paths[@]}"; do
				grep -vFxn "$p" "$_tmp" > "$SESSION_MANIFEST" || true
				cp "$SESSION_MANIFEST" "$_tmp"
			done
			rm -f "$_tmp"
			# If manifest is empty, remove it
			if [ ! -s "$SESSION_MANIFEST" ]; then
				rm -f "$SESSION_MANIFEST"
			fi
		fi
	fi
	;;
reset)
	shift
	for arg in "$@"; do
		if [ "$arg" = "--hard" ]; then
			echo "git-wrapper: BLOCKED 'git reset --hard' (parallel-agent safety)." >&2
			echo "  This can destroy other agents' uncommitted work." >&2
			exit 1
		fi
	done
	# git reset HEAD (unstage all) is fine — clean up our manifest too
	if [ $# -eq 0 ] || [[ "${1:-}" == HEAD* ]]; then
		_cleanup_session
	fi
	command git reset "$@"
	;;
stash)
	echo "git-wrapper: BLOCKED 'git stash' (parallel-agent safety)." >&2
	echo "  Stashing hides other agents' uncommitted changes." >&2
	exit 1
	;;
status|diff)
	# Pass through read-only commands
	command git "$@"
	;;
*)
	command git "$@"
	;;
esac
