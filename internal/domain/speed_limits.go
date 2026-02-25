package domain

// SpeedLimits defines global transfer rate limits in KiB/s.
// A value of 0 means unlimited.
type SpeedLimits struct {
	DownloadKiB int64
	UploadKiB   int64
}
