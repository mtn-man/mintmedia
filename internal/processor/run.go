package processor

import "context"

// ProcessEach calls proc.Process and invokes onResult for every result.
// If Process streams results via OnResult, onResult is called for each as it arrives.
// If Process returns without having streamed any results, onResult is called for each
// result in the returned slice. The caller must not set req.OnResult.
// Returns the error from Process.
func ProcessEach(ctx context.Context, proc Processor, req Request, onResult func(Result)) error {
	streamed := false
	req.OnResult = func(r Result) {
		streamed = true
		onResult(r)
	}
	results, err := proc.Process(ctx, req)
	if !streamed {
		for _, r := range results {
			onResult(r)
		}
	}
	return err
}
