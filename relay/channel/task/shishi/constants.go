package shishi

const (
	ChannelName        = "shishi-universal"
	DefaultBaseURL     = "http://154.40.44.244:3000"
	defaultVideoSecond = 4
)

// The upstream publishes models continuously. Administrators configure the
// available model IDs on each channel instead of relying on a stale built-in list.
var ModelList = []string{}
