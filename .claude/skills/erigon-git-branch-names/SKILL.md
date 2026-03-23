---
name: erigon-git-branch-names
description: Naming conventions for Erigon git branches and PRs. Use this whenever creating a branch, naming a branch, asking what branch to base work on, or writing a PR title for a cherry-pick.
---

# Branch Naming Conventions

## Release branches

| Branch | Purpose |
|--------|---------|
| `release/3.3` | Stable 3.3.x — tags cut here for v3.3.* releases |
| `release/3.4` | Stable 3.4.x — tags cut here for v3.4.* releases |
| `main` | Future release (next minor, currently 3.5) |

When `main` stabilizes, a new `release/3.5` branch is cut from it.

## Developer branches (alex's convention)

Format: `alex/<short_desc>_<release_num>[_suffix]`

The **release number** is two digits without the dot — `34` = release/3.4, `35` = main (future 3.5), `33` = release/3.3, etc.

```
alex/seg_header_meta2_34        # work on release/3.4
alex/integ_trie_root_35         # work targeting main (3.5)
alex/all7_34_dbg                # debug variant of all7_34
alex/all7_34_auto               # auto-generated variant
alex/seg_header_meta_34_dbg     # debug build for testing
```

### Common suffixes

| Suffix | Meaning |
|--------|---------|
| `_dbg` | Debug or diagnostic variant (extra logging, assertions, etc.) |
| `_auto` | Auto-generated or automated iteration of a branch |
| `_<n>` | Numbered revision (e.g., `al_reset2_34`, `al_reset3_34`) |

Short descriptions use underscores: `seg_header_meta`, `all7`, `no_bg_ind`, `mdbx_zero_alloc_count`.

## Other naming patterns seen in the repo

| Pattern | Example | Context |
|---------|---------|---------|
| `agent-fix/<desc>` | `agent-fix/revert-19508-txnum-field` | AI agent–created fixes |
| `fix/<desc>` | `fix/retire-blocks` | Manual bugfix branches |
| `feat/<desc>` | `feat/persistent-shared-domains` | Feature branches |
| `<number>-<desc>` | `16776-eth_getproof-nil-ptr` | Issue-linked branches |
| `<user>/<desc>` | `anacrolix/torrent-update` | Other contributors |

## PR title conventions (from CLAUDE.md)

**Commit messages** are prefixed with the packages modified:
```
eth, rpc: make trace configs optional
db/seg: V2 files only created via NewWriter
```

**Cherry-pick PRs** to a release branch get a `[rX.Y]` prefix:
```
[r3.4] eth, rpc: make trace configs optional
[r3.4] execution/tests: skip on Windows (#20072)
```

## Choosing a base branch

- Bug that needs to go into the current stable release → base on `release/3.4`
- New feature or work that won't ship until the next minor → base on `main`
- Backport / cherry-pick → branch off the target release branch, prefix PR title with `[rX.Y]`