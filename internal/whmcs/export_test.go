package whmcs

import "time"

// BackoffForTest exposes the retry delay calculation to the package's external
// test, so the bound can be asserted without exporting it to callers.
func BackoffForTest(attempt int) time.Duration { return backoff(attempt) }
