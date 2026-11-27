//go:build windows

package handlers

import "context"

func init() {
	adapterTargetCheck = checkAdapterTargetWindows
}

// checkAdapterTargetWindows confirms the adapter exists and is not virtual.
// Link state is deliberately not checked here: the metadata cache can be up to
// a minute old, and buildSwitchScript re-reads Status live before changing
// anything, so a stale "Disconnected" would only cause false rejections.
func checkAdapterTargetWindows(ctx context.Context, name string, allowVirtual bool) error {
	metas, err := adapterMetadataList(ctx)
	if err != nil || len(metas) == 0 {
		// A metadata failure must not block a legitimate switch; the script
		// fails with adapter-not-found on its own.
		return nil
	}
	return matchAdapterTarget(metas, name, allowVirtual)
}
