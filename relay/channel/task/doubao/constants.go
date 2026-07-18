package doubao

const (
	ByteforBaseURL                = "https://k7q9m2x4a8z3.bytefor.com"
	byteforDefaultDurationSeconds = 15
)

var doubaoModelList = []string{
	"doubao-seedance-1-0-pro-250528",
	"doubao-seedance-1-0-lite-t2v",
	"doubao-seedance-1-0-lite-i2v",
	"doubao-seedance-1-5-pro-251215",
	"doubao-seedance-2-0-260128",
	"doubao-seedance-2-0-fast-260128",
}

var byteforModelList = []string{
	"bytefor-2.0-fast-real-priority",
	"bytefor-2.0-fast",
	"bytefor-2.0",
	"bytefor-2.0-pro",
	"bytefor-2.0-real-priority",
}

var ModelList = append(append([]string{}, doubaoModelList...), byteforModelList...)

var ChannelName = "doubao-video"

// videoInputRatioMap 视频输入折扣比率（含视频单价 / 不含视频单价）。
// 管理员应将 ModelRatio 设置为"不含视频"的较高费率，
// 系统在检测到视频输入时自动乘以此折扣。
var videoInputRatioMap = map[string]float64{
	"doubao-seedance-2-0-260128":      28.0 / 46.0, // ~0.6087
	"doubao-seedance-2-0-fast-260128": 22.0 / 37.0, // ~0.5946
}

func GetVideoInputRatio(modelName string) (float64, bool) {
	r, ok := videoInputRatioMap[modelName]
	return r, ok
}

func isByteforModel(modelName string) bool {
	for _, candidate := range byteforModelList {
		if modelName == candidate {
			return true
		}
	}
	return false
}
