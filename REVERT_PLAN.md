# REVERT PLAN: Fixing the 8553d32 Disaster

**Status**: READY TO EXECUTE
**Target**: Revert to working state 829b7a5 while preserving all investigation work
**Last Updated**: 2025-09-28

## Executive Summary

Commit `8553d32` "Add freeze-frame test debugging and fix race conditions with expert-provided credit system and commit-first scheduler" completely broke the I/O system with over-engineering. We need to revert ALL the Go code changes while preserving our investigation documentation.

## Revert Strategy

### ✅ PRESERVE (Documentation & Investigation)
- `CURRENT_PROBLEM.md` - Our root cause analysis
- `CURRENT_THEORY.md` - Investigation history
- `DEBUGGING_METHODOLOGY.md` - Lessons learned
- `INVESTIGATION_CODE_CHANGES.md` - Code analysis
- `CLAUDE.md` - Update with lessons learned
- All test scripts in `scripts/`
- All `.sh` debug scripts

### ⚠️ REVERT (Go Code - Main Culprits)
- `internal/queue/runner.go` - **COMPLETELY REVERT** to 829b7a5
- `internal/uring/minimal.go` - **REVERT CHANGES** to 829b7a5
- `Makefile` - Remove `vm-freezeframe` target if not needed

### 🔍 SPECIFIC REVERT TARGETS

#### 1. `internal/queue/runner.go` - Complete Revert
**Problem**: 8553d32 destroyed the working I/O system with:
- Async initialization deadlock (`waitAndStartDataPlane()`)
- Over-complex 4-state tag machine
- Commit-first scheduler deadlock
- Dual goroutine race conditions
- Complex atomic credit tracking

**Solution**: Revert to 829b7a5 simple, working pattern

#### 2. `internal/uring/minimal.go` - Revert Changes
**Problem**: 8553d32 added complex batch support and retry queues
**Solution**: Revert to simple, working pattern

## Execution Plan

### Step 1: Save Current Investigation Work ✅
- All `.md` files are already updated with root cause analysis
- Investigation scripts preserved

### Step 2: Revert Core Files
```bash
# Get the working files from 829b7a5
git show 829b7a5:internal/queue/runner.go > runner_working.go
git show 829b7a5:internal/uring/minimal.go > minimal_working.go

# Replace the broken files
cp runner_working.go internal/queue/runner.go
cp minimal_working.go internal/uring/minimal.go

# Remove any new files that shouldn't exist
rm -f test-freezeframe.sh  # If it exists
```

### Step 3: Update Makefile
```bash
# Remove vm-freezeframe target if it was added
```

### Step 4: Build and Test
```bash
make build
make vm-simple-e2e  # MUST PASS
```

### Step 5: Update Documentation
- Update `CLAUDE.md` with critical lessons learned
- Update `TODO.md` with current status

### Step 6: Commit the Revert
```bash
git add .
git commit -m "REVERT: Remove catastrophic over-engineering from 8553d32

This commit reverts the disastrous 'expert credit system' changes that
completely broke I/O operations. The over-engineered async initialization
and complex state machine introduced multiple deadlocks.

Reverted files:
- internal/queue/runner.go - back to simple working pattern
- internal/uring/minimal.go - removed complex batching

Working state verified: make vm-simple-e2e passes

Root cause analysis preserved in:
- CURRENT_PROBLEM.md - breaking commit identified
- CURRENT_THEORY.md - investigation history
- DEBUGGING_METHODOLOGY.md - lessons learned

LESSON: Simple working code > complex 'expert' solutions
RULE: Never check in code that breaks vm-simple-e2e"
```

## Expected Results

After revert:
- ✅ `make vm-simple-e2e` should pass 100% of the time
- ✅ Device creation works
- ✅ I/O operations complete successfully
- ✅ No hanging processes
- ✅ Clean, simple, maintainable code

## Risk Mitigation

### Backup Current State
```bash
git stash push -m "backup before revert"
```

### Test Working State First
```bash
git checkout 829b7a5
make build
make vm-simple-e2e  # Verify this still works
git checkout HEAD  # Back to current
```

### Validation Checklist
- [ ] Files reverted to 829b7a5 state
- [ ] Investigation docs preserved and updated
- [ ] `make build` succeeds
- [ ] `make vm-simple-e2e` passes
- [ ] No hanging processes
- [ ] Clean commit message explaining the revert

## What NOT To Do

❌ **DO NOT** try to "fix" the over-engineered mess in 8553d32
❌ **DO NOT** keep any of the "expert credit system" code
❌ **DO NOT** keep the async initialization pattern
❌ **DO NOT** keep the complex 4-state tag machine
❌ **DO NOT** lose our investigation documentation

## Success Criteria

The revert is successful when:
1. `make vm-simple-e2e` passes consistently
2. All investigation work is preserved
3. Code is back to simple, maintainable state
4. Lessons learned are documented
5. Never make this mistake again

---

*This revert plan ensures we get back to a working state quickly while preserving all the valuable investigation work that identified the root cause.*