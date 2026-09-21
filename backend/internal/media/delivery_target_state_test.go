package media

import "testing"

// TestDeliveryTargetStreamableAndReadyVideoDiffferOnlyOnPlayable pins the two
// video predicates against each other. They are deliberately separate names for
// two separate questions — "can this be watched now" and "did the whole ladder
// finish" — and the only input on which they may ever disagree is PLAYABLE.
//
// Both still demand persisted rendition evidence, and neither accepts a
// non-video kind. D-105 / Option 3A moved the authenticated Admin review player
// from the second predicate to the first; if that distinction ever collapses,
// this test is what notices.
func TestDeliveryTargetStreamableAndReadyVideoDifferOnlyOnPlayable(t *testing.T) {
	for _, tc := range []struct {
		name          string
		target        deliveryTarget
		wantReady     bool
		wantStreamble bool
	}{
		{
			name:          "ready video with renditions",
			target:        deliveryTarget{kind: KindVideo, state: StateReady, hasRenditions: true},
			wantReady:     true,
			wantStreamble: true,
		},
		{
			name:          "playable video with renditions is streamable but not ladder-complete",
			target:        deliveryTarget{kind: KindVideo, state: StatePlayable, hasRenditions: true},
			wantReady:     false,
			wantStreamble: true,
		},
		{
			name:          "playable video without renditions has nothing to serve",
			target:        deliveryTarget{kind: KindVideo, state: StatePlayable},
			wantReady:     false,
			wantStreamble: false,
		},
		{
			name:          "ready video without renditions has nothing to serve",
			target:        deliveryTarget{kind: KindVideo, state: StateReady},
			wantReady:     false,
			wantStreamble: false,
		},
		{
			name:          "processing video is neither",
			target:        deliveryTarget{kind: KindVideo, state: StateProcessing, hasRenditions: true},
			wantReady:     false,
			wantStreamble: false,
		},
		{
			name:          "process-failed video is neither",
			target:        deliveryTarget{kind: KindVideo, state: StateProcessFailed, hasRenditions: true},
			wantReady:     false,
			wantStreamble: false,
		},
		{
			name:          "ready resource is not video",
			target:        deliveryTarget{kind: KindResource, state: StateReady, hasRenditions: true},
			wantReady:     false,
			wantStreamble: false,
		},
		{
			name:          "playable preview never becomes streamable video",
			target:        deliveryTarget{kind: KindPreview, state: StatePlayable, hasRenditions: true},
			wantReady:     false,
			wantStreamble: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.target.readyVideo(); got != tc.wantReady {
				t.Errorf("readyVideo() = %t, want %t", got, tc.wantReady)
			}
			if got := tc.target.streamableVideo(); got != tc.wantStreamble {
				t.Errorf("streamableVideo() = %t, want %t", got, tc.wantStreamble)
			}
			// streamableVideo is a strict widening of readyVideo: anything
			// ladder-complete is also watchable now, never the other way round.
			if tc.target.readyVideo() && !tc.target.streamableVideo() {
				t.Error("a ready video was not streamable")
			}
		})
	}
}
