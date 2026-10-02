package convert

// Version is the application version shown in the interface and in build metadata.
const Version = "0.1.0"

// Kind is the broad type of a media file. It decides which output formats are offered.
type Kind string

const (
	KindImage Kind = "image"
	KindAudio Kind = "audio"
	KindVideo Kind = "video"
)

// Item statuses, in the order an item normally moves through them.
const (
	StatusChecking    = "checking"
	StatusReady       = "ready"
	StatusUnsupported = "unsupported"
	StatusQueued      = "queued"
	StatusConverting  = "converting"
	StatusDone        = "done"
	StatusFailed      = "failed"
	StatusCancelled   = "cancelled"
	StatusSkipped     = "skipped"
)

// Batch states.
const (
	BatchIdle       = "idle"
	BatchRunning    = "running"
	BatchCancelling = "cancelling"
	BatchFinished   = "finished"
)

// Media is what the engine reported about one input file.
type Media struct {
	Format       string
	Kind         Kind
	Width        int
	Height       int
	DurationMs   int64
	VideoCodec   string
	PixFmt       string
	Alpha        bool
	Rotation     int
	Interlaced   bool
	HDR          bool
	FrameRate    float64
	Animated     bool
	HasAudio     bool
	AudioCodec   string
	AudioProfile string
	Channels     int
	SampleRate   int
	AudioBits    int
	FloatAudio   bool
	CoverArt     bool

	// Stream numbers inside the file, used to pick exactly these streams when converting.
	videoIndex int
	audioIndex int
	coverIndex int
	// sourceExt is the lower-case extension of the file the facts came from.
	sourceExt string
}

// Item is one file in the list. The exported fields are sent to the interface.
type Item struct {
	ID          string  `json:"id"`
	Path        string  `json:"path"`
	Name        string  `json:"name"`
	Folder      string  `json:"folder"`
	Size        int64   `json:"size"`
	Kind        Kind    `json:"kind"`
	Status      string  `json:"status"`
	Source      string  `json:"source"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
	DurationMs  int64   `json:"durationMs"`
	Target      string  `json:"target"`
	TargetLabel string  `json:"targetLabel"`
	Progress    float64 `json:"progress"`
	OutputPath  string  `json:"outputPath"`
	OutputName  string  `json:"outputName"`
	OutputSize  int64   `json:"outputSize"`
	Error       string  `json:"error"`
	Detail      string  `json:"detail"`
	Warning     string  `json:"warning"`
	Thumb       bool    `json:"thumb"`

	media Media
	// note is a warning found while inspecting the file. It survives a change of target.
	note string
}

// Option is one entry in a format picker.
type Option struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Group     string `json:"group"`
	Available bool   `json:"available"`
	Reason    string `json:"reason"`
}

// KindState is the format picker for one kind of file that is present in the list.
type KindState struct {
	Kind    Kind     `json:"kind"`
	Label   string   `json:"label"`
	Count   int      `json:"count"`
	Target  string   `json:"target"`
	Options []Option `json:"options"`
}

// FormatRow is one line of the "reads and writes" table shown when the list is empty.
type FormatRow struct {
	Kind   Kind     `json:"kind"`
	Label  string   `json:"label"`
	Reads  []string `json:"reads"`
	Writes []string `json:"writes"`
}

// DestinationState says where converted files are saved.
type DestinationState struct {
	Mode   string `json:"mode"`
	Folder string `json:"folder"`
}

// BatchState summarises the current or last run.
type BatchState struct {
	State     string `json:"state"`
	Total     int    `json:"total"`
	Done      int    `json:"done"`
	Failed    int    `json:"failed"`
	Skipped   int    `json:"skipped"`
	Cancelled int    `json:"cancelled"`
	CurrentID string `json:"currentId"`
}

// Snapshot is the complete state the interface renders. It is polled, never pushed.
type Snapshot struct {
	Version     string           `json:"version"`
	Engine      string           `json:"engine"`
	State       string           `json:"state"`
	SetupError  string           `json:"setupError"`
	Items       []Item           `json:"items"`
	Kinds       []KindState      `json:"kinds"`
	Destination DestinationState `json:"destination"`
	Batch       BatchState       `json:"batch"`
	Formats     []FormatRow      `json:"formats"`
	Revision    uint64           `json:"revision"`
	// Notice is a message for the user about something that happened outside the window,
	// such as files handed over by Explorer. NoticeSeq changes with every new notice.
	Notice      string `json:"notice"`
	NoticeError bool   `json:"noticeError"`
	NoticeSeq   uint64 `json:"noticeSeq"`
}

// Skipped explains why a path was not added to the list.
type Skipped struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

// AddResult reports what happened to the paths the user dropped or chose. Message is a
// ready-made sentence about the skipped ones, or empty when everything was added.
type AddResult struct {
	Added   int       `json:"added"`
	Skipped []Skipped `json:"skipped"`
	Message string    `json:"message"`
}

// Conflict is an output name that already exists in the destination.
type Conflict struct {
	ItemID string `json:"itemId"`
	Name   string `json:"name"`
	Folder string `json:"folder"`
}

// StartResult is returned by Start. When Conflicts is not empty nothing was started
// and the caller has to ask the user how to handle the existing files.
type StartResult struct {
	Started   bool       `json:"started"`
	Conflicts []Conflict `json:"conflicts"`
}

// Capabilities records which optional encoders work on this computer.
type Capabilities struct {
	H264       bool
	H264Reason string
}
