package sync

import "time"

// RemoteWins compares a local memo with a remote page. Revision first, then the
// client Updated timestamp, then the writer device lexicographically.
func RemoteWins(localRevision int64, localUpdated time.Time, localDevice string, remoteRevision int64, remoteUpdated time.Time, remoteDevice string) bool {
	if remoteRevision != localRevision {
		return remoteRevision > localRevision
	}
	if !remoteUpdated.Equal(localUpdated) {
		return remoteUpdated.After(localUpdated)
	}
	return remoteDevice > localDevice
}
