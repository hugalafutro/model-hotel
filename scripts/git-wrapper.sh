#!/usr/bin/env bash
# git-wrapper.sh: intercepts dangerous git commands in parallel-agent environments.
# Usage: alias git=this-script, or set as GIT_EXEC_PATH or use via agent config.
#
# Blocks:
#   - git commit --amend (sets sentinel for pre-commit hook to detect)
#   - git add . / git add -A / git add --all (forces explicit file paths)
#   - git reset (without --soft or explicit ref, warns)
#   - git stash (blocked entirely)

set -euo pipefail

# Bypass: set GIT_UNSAFE=1 to skip all safety checks (for human operators).
# Agents should never set this — it exists so repo owners can amend, stash, etc.
if [ "${GIT_UNSAFE:-0}" = "1" ]; then
	command git "$@"
	exit $?
fi

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || true)"

case "${1:-}" in
commit)
	shift
	for arg in "$@"; do
		if [ "$arg" = "--amend" ]; then
			# Create sentinel file so pre-commit hook can detect amend
			touch "$REPO_ROOT/.git/AMEND_IN_PROGRESS"
			# Run git commit; hook will block and clean up sentinel
			command git commit "$@"
			exit $?
		fi
	done
	command git commit "$@"
	;;
add)
	shift
	if [ $# -eq 0 ]; then
		echo "git-wrapper: BLOCKED bare 'git add' (no files specified)." >&2
		echo "  Specify exact files: git add <file1> <file2>" >&2
		exit 1
	fi
	for arg in "$@"; do
		case "$arg" in
			.|-A|--all)
				echo "git-wrapper: BLOCKED 'git add $arg' — broad staging is not allowed." >&2
				echo "  Other agents may have uncommitted changes. Specify exact files." >&2
				exit 1
				;;
		esac
	done
	command git add "$@"
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
	command git reset "$@"
	;;
stash)
	echo "git-wrapper: BLOCKED 'git stash' (parallel-agent safety)." >&2
	echo "  Stashing hides other agents' uncommitted changes." >&2
	exit 1
	;;
*)
	command git "$@"
	;;
esac
