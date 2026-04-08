package stagedsync

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/erigontech/erigon/common/dbg"
	"github.com/erigontech/erigon/common/log/v3"
	"github.com/erigontech/erigon/db/kv"
	"github.com/erigontech/erigon/db/rawdb"
	"github.com/erigontech/erigon/execution/protocol/rules"
	"github.com/erigontech/erigon/execution/state"
	"github.com/erigontech/erigon/execution/types"
)

func CreateBAL(blockNum uint64, txIO *state.VersionedIO, dataDir string) types.BlockAccessList {
	bal := txIO.AsBlockAccessList()
	if dbg.TraceBlockAccessLists {
		writeBALToFile(bal, blockNum, dataDir)
	}
	return bal
}

// writeBALToFile writes the Block Access List to a text file for debugging/analysis
func writeBALToFile(bal types.BlockAccessList, blockNum uint64, dataDir string) {
	if dataDir == "" {
		return
	}

	balDir := filepath.Join(dataDir, "bal")
	if err := os.MkdirAll(balDir, 0755); err != nil {
		log.Warn("Failed to create BAL directory", "dir", balDir, "error", err)
		return
	}

	filename := filepath.Join(balDir, fmt.Sprintf("bal_block_%d.txt", blockNum))

	file, err := os.Create(filename)
	if err != nil {
		log.Warn("Failed to create BAL file", "blockNum", blockNum, "error", err)
		return
	}
	defer file.Close()

	fmt.Fprintf(file, "Block Access List for Block %d\n", blockNum)
	bal.DebugPrint(file)
}

func ProcessBAL(tx kv.TemporalRwTx, h *types.Header, vio *state.VersionedIO, dataDir string) error {
	if h == nil {
		return nil
	}
	blockNum := h.Number.Uint64()
	blockHash := h.Hash()
	bal := CreateBAL(blockNum, vio, dataDir)
	err := bal.Validate()
	if err != nil {
		return fmt.Errorf("block %d: invalid computed block access list: %w", blockNum, err)
	}
	if err := bal.ValidateMaxItems(h.GasLimit); err != nil {
		return fmt.Errorf("%w, block %d: %w", rules.ErrInvalidBlock, blockNum, err)
	}
	log.Debug("bal", "blockNum", blockNum, "hash", bal.Hash())
	if h.BlockAccessListHash == nil {
		return fmt.Errorf("block %d: missing block access list hash", blockNum)
	}
	headerBALHash := *h.BlockAccessListHash
	dbBALBytes, err := rawdb.ReadBlockAccessListBytes(tx, blockHash, blockNum)
	if err != nil {
		return fmt.Errorf("block %d: read stored block access list: %w", blockNum, err)
	}
	// BAL data may not be stored for blocks downloaded via backward
	// block downloader (p2p sync) since it does not carry BAL sidecars.
	// Remove after eth/71 has been implemented.
	if dbBALBytes != nil {
		dbBAL, err := types.DecodeBlockAccessListBytes(dbBALBytes)
		if err != nil {
			return fmt.Errorf("block %d: read stored block access list: %w", blockNum, err)
		}
		if err = dbBAL.Validate(); err != nil {
			return fmt.Errorf("block %d: db block access list is invalid: %w", blockNum, err)
		}
		if err = dbBAL.ValidateMaxItems(h.GasLimit); err != nil {
			return fmt.Errorf("block %d: stored block access list exceeds max items: %w", blockNum, err)
		}

		if headerBALHash != dbBAL.Hash() {
			log.Info(fmt.Sprintf("bal from block: %s", dbBAL.DebugString()))
			return fmt.Errorf("block %d: invalid block access list, hash mismatch: got %s expected %s", blockNum, dbBAL.Hash(), headerBALHash)
		}
	}
	// Always validate computed BAL against header. The BalancePath cross-check
	// in VersionMap.validateRead ensures deterministic parallel execution even
	// without a stored BAL body (HasBAL=false), so the computed BAL is accurate.
	if headerBALHash != bal.Hash() {
		// Log per-tx IO stats from the VersionedIO to help diagnose index mismatches.
		if vio != nil {
			maxTx := vio.Len() - 1
			log.Warn("BAL mismatch: blockIO stats",
				"block", blockNum,
				"maxTxIndex", maxTx,
				"totalReads", vio.ReadCount(),
				"totalWrites", vio.WriteCount())
			// Log first few txIndex write counts
			for ti := -1; ti <= min(maxTx, 5); ti++ {
				ws := vio.WriteSet(ti)
				rs := vio.ReadSet(ti)
				log.Warn("BAL mismatch: txIO",
					"block", blockNum,
					"txIndex", ti,
					"reads", rs.Len(),
					"writes", len(ws))
			}
		}
		// Log first few per-account balance/nonce index diffs
		if dbBALBytes != nil {
			if dbBAL2, decErr := types.DecodeBlockAccessListBytes(dbBALBytes); decErr == nil && dbBAL2 != nil {
				diffCount := 0
				ci, si := 0, 0
				for ci < len(bal) && si < len(dbBAL2) && diffCount < 3 {
					ca, sa := bal[ci], dbBAL2[si]
					cmp := ca.Address.Cmp(sa.Address)
					if cmp < 0 {
						log.Warn("BAL mismatch: addr only in computed", "block", blockNum, "addr", ca.Address,
							"balanceChanges", len(ca.BalanceChanges), "nonceChanges", len(ca.NonceChanges))
						ci++
						diffCount++
					} else if cmp > 0 {
						log.Warn("BAL mismatch: addr only in stored", "block", blockNum, "addr", sa.Address,
							"balanceChanges", len(sa.BalanceChanges), "nonceChanges", len(sa.NonceChanges))
						si++
						diffCount++
					} else {
						// Same address — compare first balance/nonce index
						if len(ca.BalanceChanges) > 0 && len(sa.BalanceChanges) > 0 &&
							ca.BalanceChanges[0].Index != sa.BalanceChanges[0].Index {
							log.Warn("BAL mismatch: balance index diff", "block", blockNum, "addr", ca.Address,
								"computedIdx", ca.BalanceChanges[0].Index, "storedIdx", sa.BalanceChanges[0].Index,
								"computedVal", fmt.Sprintf("0x%x", &ca.BalanceChanges[0].Value),
								"storedVal", fmt.Sprintf("0x%x", &sa.BalanceChanges[0].Value))
							diffCount++
						}
						if len(ca.NonceChanges) > 0 && len(sa.NonceChanges) > 0 &&
							ca.NonceChanges[0].Index != sa.NonceChanges[0].Index {
							log.Warn("BAL mismatch: nonce index diff", "block", blockNum, "addr", ca.Address,
								"computedIdx", ca.NonceChanges[0].Index, "storedIdx", sa.NonceChanges[0].Index)
							diffCount++
						}
						ci++
						si++
					}
				}
			}
		}
		// Log BAL debug strings so CI output captures the field-by-field diff
		log.Warn("BAL mismatch: computed BAL", "block", blockNum, "hash", bal.Hash(), "debug", bal.DebugString())
		if dbBALBytes != nil {
			if dbBAL2, err := types.DecodeBlockAccessListBytes(dbBALBytes); err == nil && dbBAL2 != nil {
				log.Warn("BAL mismatch: stored BAL", "block", blockNum, "hash", dbBAL2.Hash(), "debug", dbBAL2.DebugString())
			}
		}
		if dataDir != "" {
			balDir := filepath.Join(dataDir, "bal")
			if err := os.MkdirAll(balDir, 0o755); err != nil {
				log.Warn("failed to create BAL debug directory", "dir", balDir, "err", err)
			} else {
				computedPath := filepath.Join(balDir, fmt.Sprintf("computed_bal_%d.txt", blockNum))
				if err := os.WriteFile(computedPath, []byte(bal.DebugString()), 0o644); err != nil {
					log.Warn("failed to write computed BAL debug file", "path", computedPath, "err", err)
				}
				dbBAL2, err := types.DecodeBlockAccessListBytes(dbBALBytes)
				if err != nil {
					log.Warn("failed to decode stored BAL for debug dump", "err", err)
				} else if dbBAL2 != nil {
					storedPath := filepath.Join(balDir, fmt.Sprintf("stored_bal_%d.txt", blockNum))
					if err := os.WriteFile(storedPath, []byte(dbBAL2.DebugString()), 0o644); err != nil {
						log.Warn("failed to write stored BAL debug file", "path", storedPath, "err", err)
					}
				}
			}
		}
		return fmt.Errorf("%w, block=%d: block access list mismatch: got %s expected %s", rules.ErrInvalidBlock, blockNum, bal.Hash(), headerBALHash)
	}
	return nil
}
