package convert

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Application states.
const (
	StateStarting = "starting"
	StateReady    = "ready"
	StateFailed   = "failed"
)

// Conflict policies for Start.
const (
	PolicyAsk     = ""
	PolicyKeep    = "keep"
	PolicyReplace = "replace"
)

const (
	maxItems         = 500
	thumbnailTimeout = 20 * time.Second
	selfTestTimeout  = 30 * time.Second
	missingH264      = "MP4, MOV and MKV need the H.264 encoder that comes with Windows, and it did not start on this PC. Windows N editions need the Media Feature Pack."
	unreadableFile   = "Convert Me could not read this file. It may be damaged, or in a format that is not supported yet."
)

// Service owns the file list and runs conversions one at a time. Every method is safe to
// call from any goroutine. The interface only ever sees copies made by Status.
type Service struct {
	mu          sync.Mutex
	root        string
	dataDir     string
	run         Runner
	engine      Engine
	system      SystemImages
	engineLabel string
	state       string
	setupErr    string
	caps        Capabilities
	decoders    map[string]bool
	settings    Settings
	items       []*Item
	batch       BatchState
	revision    uint64
	thumbs      map[string][]byte
	notice      string
	noticeError bool
	noticeSeq   uint64

	base       context.Context
	stop       context.CancelFunc
	cancel     context.CancelFunc
	done       chan struct{}
	background sync.WaitGroup
	probeSlots chan struct{}
	thumbSlots chan struct{}
}

// NewService prepares a service. root is the folder that contains "runtime", dataDir is
// where settings and short-lived work files go.
func NewService(root, dataDir string, runner Runner) *Service {
	base, stop := context.WithCancel(context.Background())
	return &Service{
		root: root, dataDir: dataDir, run: runner, system: newSystemImages(),
		state: StateStarting, engineLabel: "FFmpeg",
		settings: defaultSettings(), batch: BatchState{State: BatchIdle},
		thumbs: map[string][]byte{}, decoders: map[string]bool{},
		base: base, stop: stop,
		probeSlots: make(chan struct{}, 4), thumbSlots: make(chan struct{}, 2),
	}
}

// Initialize checks the bundled engine and finds out what it can do on this computer.
func (s *Service) Initialize(manifestData []byte) error {
	manifest, err := ParseManifest(manifestData)
	if err != nil {
		return s.fail(err)
	}
	if err := VerifyManifest(s.base, s.root, manifest); err != nil {
		return s.fail(fmt.Errorf("the conversion engine is missing or damaged (%w). Extract the complete Convert Me download again", err))
	}
	engine := Engine{
		FFmpeg:  filepath.Join(s.root, filepath.FromSlash(ffmpegAsset)),
		FFprobe: filepath.Join(s.root, filepath.FromSlash(ffprobeAsset)),
		Run:     s.run,
	}
	decoders, err := engine.Decoders(s.base)
	if err != nil {
		return s.fail(fmt.Errorf("the conversion engine did not start: %w", err))
	}
	work := filepath.Join(s.dataDir, "work")
	// Only this app's own work folder is cleared. It holds nothing but colour tables and
	// the copies that Windows makes of HEIC photos while they are converted.
	_ = os.RemoveAll(work)
	if err := os.MkdirAll(work, 0700); err != nil {
		return s.fail(fmt.Errorf("the application data folder could not be created: %w", err))
	}
	caps := engine.SelfTest(s.base)
	caps.HEIC, caps.HEICReason = checkSystemImages(s.system)
	settings := loadSettings(s.settingsPath())
	for _, kind := range kindOrder {
		settings.Targets[kind] = firstAvailable(kind, settings.Targets[kind], caps)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.engine, s.decoders, s.caps, s.settings = engine, decoders, caps, settings
	s.engineLabel = manifest.EngineLabel()
	s.state = StateReady
	s.revision++
	return nil
}

func (s *Service) fail(err error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = StateFailed
	s.setupErr = sentence(err.Error())
	s.revision++
	return err
}

func (s *Service) settingsPath() string {
	return filepath.Join(s.dataDir, "settings.json")
}

// Decoders asks the engine which codecs it can read, so the app never has a second list
// that could drift away from what was really built.
func (e Engine) Decoders(ctx context.Context) (map[string]bool, error) {
	data, err := e.Run(ctx, e.FFmpeg, []string{"-hide_banner", "-v", "error", "-decoders"}, nil)
	if err != nil {
		return nil, err
	}
	decoders := parseDecoders(string(data))
	if len(decoders) == 0 {
		return nil, errors.New("the engine reported no decoders")
	}
	return decoders, nil
}

func parseDecoders(text string) map[string]bool {
	decoders := map[string]bool{}
	listing := false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "------") {
			listing = true
			continue
		}
		fields := strings.Fields(line)
		if !listing || len(fields) < 2 {
			continue
		}
		decoders[fields[1]] = true
		// A decoder whose name differs from its codec says so: "mp3float ... (codec mp3)".
		if i := strings.LastIndex(line, "(codec "); i >= 0 {
			decoders[strings.TrimSuffix(line[i+len("(codec "):], ")")] = true
		}
	}
	return decoders
}

// SelfTest finds out whether the Windows H.264 encoder can be used.
func (e Engine) SelfTest(ctx context.Context) Capabilities {
	testCtx, cancel := context.WithTimeout(ctx, selfTestTimeout)
	defer cancel()
	if _, err := e.Run(testCtx, e.FFmpeg, h264SelfTestArgs(), nil); err != nil {
		return Capabilities{H264Reason: missingH264}
	}
	return Capabilities{H264: true}
}

func newID() string {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("%016x", time.Now().UnixNano())
	}
	return hex.EncodeToString(raw)
}

func (s *Service) find(id string) *Item {
	for _, item := range s.items {
		if item.ID == id {
			return item
		}
	}
	return nil
}

func (s *Service) busy() bool {
	return s.batch.State == BatchRunning || s.batch.State == BatchCancelling
}

// Status returns a copy of everything the interface shows.
func (s *Service) Status() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot := Snapshot{
		Version: Version, Engine: s.engineLabel, State: s.state, SetupError: s.setupErr,
		Items:       make([]Item, len(s.items)),
		Kinds:       []KindState{},
		Destination: DestinationState{Mode: s.settings.DestinationMode, Folder: s.settings.DestinationFolder},
		Batch:       s.batch,
		Formats:     FormatTable(s.caps),
		Revision:    s.revision,
		Notice:      s.notice, NoticeError: s.noticeError, NoticeSeq: s.noticeSeq,
	}
	counts := map[Kind]int{}
	for i, item := range s.items {
		snapshot.Items[i] = *item
		if item.Kind != "" && item.Status != StatusUnsupported {
			counts[item.Kind]++
		}
	}
	for _, kind := range kindOrder {
		if counts[kind] == 0 {
			continue
		}
		snapshot.Kinds = append(snapshot.Kinds, KindState{
			Kind: kind, Label: kindLabels[kind], Count: counts[kind],
			Target: s.settings.Targets[kind], Options: options(kind, s.caps),
		})
	}
	return snapshot
}

// AddFiles puts files in the list and starts inspecting them in the background.
// Paths that cannot be used are returned with the reason instead of being added.
func (s *Service) AddFiles(paths []string) (AddResult, error) {
	result := AddResult{Skipped: []Skipped{}}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != StateReady {
		return result, errors.New("Convert Me is not ready yet")
	}
	if s.busy() {
		return result, errors.New("wait for the current conversion to finish before adding files")
	}
	present := make(map[string]bool, len(s.items))
	for _, item := range s.items {
		present[pathKey(item.Path)] = true
	}
	for _, raw := range paths {
		path := filepath.Clean(strings.TrimSpace(raw))
		name := filepath.Base(path)
		skip := func(reason string) {
			result.Skipped = append(result.Skipped, Skipped{Name: name, Reason: reason})
		}
		info, err := checkInput(path)
		switch {
		case err != nil:
			skip(err.Error())
			continue
		case !KnownExtension(path):
			skip("not an image, audio or video type that Convert Me reads")
			continue
		case present[pathKey(path)]:
			skip("already in the list")
			continue
		case len(s.items) >= maxItems:
			skip(fmt.Sprintf("the list is full (%d files)", maxItems))
			continue
		}
		present[pathKey(path)] = true
		item := &Item{
			ID: newID(), Path: path, Name: name, Folder: filepath.Dir(path), Size: info.Size(),
			Status: StatusChecking, Source: sourceLabel(path, Media{}),
		}
		s.items = append(s.items, item)
		result.Added++
		s.background.Add(1)
		go s.inspect(item.ID, path)
	}
	if result.Added > 0 {
		s.batch = BatchState{State: BatchIdle}
		s.revision++
	}
	result.Message = SkippedSummary(result)
	return result, nil
}

// Announce leaves a message for the window. It is used when files arrive from outside the
// window, where there was no dialog to show the outcome in.
func (s *Service) Announce(text string, isError bool) {
	if text == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notice, s.noticeError = text, isError
	s.noticeSeq++
	s.revision++
}

// SkippedSummary describes the files that were not added, in one sentence.
func SkippedSummary(result AddResult) string {
	count := len(result.Skipped)
	switch count {
	case 0:
		return ""
	case 1:
		return fmt.Sprintf("Skipped %s: %s.", result.Skipped[0].Name, result.Skipped[0].Reason)
	}
	names := make([]string, 0, 3)
	sameReason := true
	for i, skipped := range result.Skipped {
		if i < 3 {
			names = append(names, skipped.Name)
		}
		sameReason = sameReason && skipped.Reason == result.Skipped[0].Reason
	}
	list := strings.Join(names, ", ")
	if count > len(names) {
		list += fmt.Sprintf(" and %d more", count-len(names))
	}
	if sameReason {
		return fmt.Sprintf("Skipped %d files (%s): %s.", count, result.Skipped[0].Reason, list)
	}
	return fmt.Sprintf("Skipped %d files that Convert Me cannot use: %s.", count, list)
}

// inspect probes one file and records what it is.
func (s *Service) inspect(id, path string) {
	defer s.background.Done()
	select {
	case s.probeSlots <- struct{}{}:
		defer func() { <-s.probeSlots }()
	case <-s.base.Done():
		return
	}
	if systemImage(path) {
		s.inspectSystemImage(id, path)
		return
	}
	media, err := s.engine.Probe(s.base, path)

	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.find(id)
	if item == nil || item.Status != StatusChecking {
		return
	}
	s.revision++
	if err != nil {
		if s.base.Err() != nil {
			return
		}
		item.Status = StatusUnsupported
		item.Error = unreadableFile
		var failure *ProcessError
		switch {
		case errors.As(err, &failure):
			item.Detail = lastLines(failure.Stderr, 12)
		default:
			// Plain errors come from this app and already say what is wrong.
			item.Error = sentence(err.Error())
		}
		return
	}
	note := ""
	if media.Kind != KindAudio && !s.decoders[media.VideoCodec] {
		item.Status = StatusUnsupported
		item.Kind = media.Kind
		item.Error = fmt.Sprintf("This file uses %s, which Convert Me cannot read yet.", codecName(media.VideoCodec))
		return
	}
	if media.HasAudio && !s.decoders[media.AudioCodec] {
		if media.Kind == KindAudio {
			item.Status = StatusUnsupported
			item.Kind = media.Kind
			item.Error = fmt.Sprintf("This file uses %s, which Convert Me cannot read yet.", codecName(media.AudioCodec))
			return
		}
		note = fmt.Sprintf("The sound uses %s, which Convert Me cannot read yet. The result will have no sound.", codecName(media.AudioCodec))
		media.HasAudio = false
	}
	item.media = media
	item.note = note
	item.Kind = media.Kind
	item.Source = sourceLabel(path, media)
	item.Width, item.Height, item.DurationMs = media.Width, media.Height, media.DurationMs
	item.Status = StatusReady
	s.assign(item)
	if media.Kind != KindAudio {
		s.background.Add(1)
		go s.thumbnail(id, path, media)
	}
}

// inspectSystemImage records a HEIC photo. Windows reads it once, small. That proves the
// photo can be read, tells its size, and gives the preview for the list.
func (s *Service) inspectSystemImage(id, path string) {
	s.mu.Lock()
	system, engine := s.system, s.engine
	can, reason := s.caps.HEIC, s.caps.HEICReason
	s.mu.Unlock()
	found := false
	if !can {
		// The codecs may have been added to Windows since Convert Me started.
		can, reason = checkSystemImages(system)
		found = can
	}
	var (
		width, height int
		thumb         []byte
		err           error
	)
	if can {
		preview := filepath.Join(s.dataDir, "work", "preview-"+id+".png")
		if err = os.MkdirAll(filepath.Dir(preview), 0700); err != nil {
			err = &SystemImageError{Message: "Convert Me could not use its work folder to read this photo.", Detail: err.Error()}
		} else {
			width, height, err = system.Export(path, preview, previewSide)
		}
		if err == nil {
			ctx, cancel := context.WithTimeout(s.base, thumbnailTimeout)
			thumb, _ = s.run(ctx, engine.FFmpeg, thumbnailArgs(preview, Media{Kind: KindImage}), nil)
			cancel()
		}
		_ = os.Remove(preview)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if found {
		s.caps.HEIC, s.caps.HEICReason = true, ""
		s.revision++
	}
	item := s.find(id)
	if item == nil || item.Status != StatusChecking || s.base.Err() != nil {
		return
	}
	s.revision++
	item.Kind = KindImage
	item.Source = heicLabel
	switch {
	case !can:
		item.Status = StatusUnsupported
		item.Error = reason
	case err != nil:
		failure := systemFailure(err)
		item.Status = StatusUnsupported
		item.Error, item.Detail = failure.Message, failure.Detail
	default:
		item.media = Media{Format: heicFormat, Kind: KindImage, Width: width, Height: height, system: true}
		item.Width, item.Height = width, height
		s.assign(item)
		if len(thumb) > 0 {
			s.thumbs[id] = thumb
			item.Thumb = true
		}
	}
}

// thumbnail makes a small preview in memory. A failure only means the row shows an icon.
func (s *Service) thumbnail(id, path string, media Media) {
	defer s.background.Done()
	select {
	case s.thumbSlots <- struct{}{}:
		defer func() { <-s.thumbSlots }()
	case <-s.base.Done():
		return
	}
	ctx, cancel := context.WithTimeout(s.base, thumbnailTimeout)
	defer cancel()
	data, err := s.run(ctx, s.engine.FFmpeg, thumbnailArgs(path, media), nil)
	if err != nil || len(data) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if item := s.find(id); item != nil {
		s.thumbs[id] = data
		item.Thumb = true
		s.revision++
	}
}

// Thumbnail returns the preview for an item, or nil.
func (s *Service) Thumbnail(id string) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.thumbs[id]
}

// assign gives an item the target currently chosen for its kind. A file that cannot or
// need not be converted to that target is marked as skipped straight away, so the list and
// the Convert button are honest before anything runs. The lock must be held.
func (s *Service) assign(item *Item) {
	target, _ := targetByID(s.settings.Targets[item.Kind])
	item.Status = StatusReady
	item.Target, item.TargetLabel = target.ID, target.Label
	item.Progress, item.OutputPath, item.OutputName, item.OutputSize = 0, "", "", 0
	item.Error, item.Detail, item.Warning = "", "", ""
	if reason := skipReason(item.media, target); reason != "" {
		item.Status = StatusSkipped
		item.Error = reason
		return
	}
	warnings := []string{}
	if item.note != "" {
		warnings = append(warnings, item.note)
	}
	if item.media.HDR && target.Kind != KindAudio {
		warnings = append(warnings, "This is HDR video. Colors can look flat after converting, because HDR is not supported yet.")
	}
	if target.Needs == needH264 && shrinksForH264(item.media) {
		if copies, _ := copyable(item.media, target); !copies {
			warnings = append(warnings, "This video is larger than 4K. It is scaled down to 4K so the result plays everywhere.")
		}
	}
	if target.ID == "gif" && item.media.Kind == KindVideo && item.media.DurationMs > 30_000 {
		warnings = append(warnings, "Long videos make very large GIF files.")
	}
	item.Warning = strings.Join(warnings, " ")
}

// skipReason says why a file cannot, or need not, be converted to a target at all.
func skipReason(media Media, target Target) string {
	switch {
	case target.Kind == KindAudio && !media.HasAudio:
		return "This video has no sound, so there is nothing to convert to " + target.Label + "."
	case target.ID == "webp" && media.Kind == KindImage && (media.Width > 16383 || media.Height > 16383):
		return "This image is too large for WebP, which allows 16383 pixels per side at most."
	case alreadyIn(media, target):
		return "Already " + target.Label + ", so there is nothing to convert."
	}
	return ""
}

// nothingNeeded explains why pressing Convert would do nothing for any file in the list.
func nothingNeeded(reasons []string) string {
	if len(reasons) == 1 {
		return reasons[0]
	}
	for _, reason := range reasons {
		if !strings.HasPrefix(reason, "Already ") {
			return "None of these files can be converted to the chosen format. Pick another format."
		}
	}
	return "These files are already in the chosen format. Pick another format to convert them."
}

func resettable(status string) bool {
	switch status {
	case StatusReady, StatusDone, StatusFailed, StatusCancelled, StatusSkipped:
		return true
	}
	return false
}

// SetTarget chooses the output format for every file of one kind.
func (s *Service) SetTarget(kind Kind, id string) error {
	s.mu.Lock()
	if s.busy() {
		s.mu.Unlock()
		return errors.New("the format cannot be changed while files are converting")
	}
	available := false
	for _, option := range options(kind, s.caps) {
		if option.ID == id {
			if !option.Available {
				s.mu.Unlock()
				return errors.New(option.Reason)
			}
			available = true
		}
	}
	if !available {
		s.mu.Unlock()
		return errors.New("that format is not available for these files")
	}
	s.settings.Targets[kind] = id
	for _, item := range s.items {
		if item.Kind == kind && resettable(item.Status) {
			item.Status = StatusReady
			s.assign(item)
		}
	}
	s.batch = BatchState{State: BatchIdle}
	s.revision++
	settings := s.copySettings()
	s.mu.Unlock()
	return saveSettings(s.settingsPath(), settings)
}

func (s *Service) copySettings() Settings {
	copied := Settings{
		Targets:           make(map[Kind]string, len(s.settings.Targets)),
		DestinationMode:   s.settings.DestinationMode,
		DestinationFolder: s.settings.DestinationFolder,
	}
	for kind, id := range s.settings.Targets {
		copied.Targets[kind] = id
	}
	return copied
}

// SetDestination chooses where converted files go. An empty folder means "next to each
// original".
func (s *Service) SetDestination(mode, folder string) error {
	if mode == DestinationFolder {
		if !filepath.IsAbs(folder) {
			return errors.New("choose a folder on this computer")
		}
		folder = filepath.Clean(folder)
		if info, err := os.Stat(folder); err != nil || !info.IsDir() {
			return errors.New("that folder is not available")
		}
	} else if mode != DestinationSource {
		return errors.New("unknown destination")
	}
	s.mu.Lock()
	if s.busy() {
		s.mu.Unlock()
		return errors.New("the destination cannot be changed while files are converting")
	}
	s.settings.DestinationMode = mode
	if mode == DestinationFolder {
		s.settings.DestinationFolder = folder
	}
	s.revision++
	settings := s.copySettings()
	s.mu.Unlock()
	return saveSettings(s.settingsPath(), settings)
}

// RemoveItem takes one file out of the list. The file itself is not touched.
func (s *Service) RemoveItem(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy() {
		return errors.New("files cannot be removed while a conversion is running")
	}
	for i, item := range s.items {
		if item.ID == id {
			s.items = append(s.items[:i], s.items[i+1:]...)
			delete(s.thumbs, id)
			s.batch = BatchState{State: BatchIdle}
			s.revision++
			return nil
		}
	}
	return nil
}

// ClearItems empties the list. Files on disk are not touched.
func (s *Service) ClearItems() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy() {
		return errors.New("the list cannot be cleared while a conversion is running")
	}
	s.items = nil
	s.thumbs = map[string][]byte{}
	s.batch = BatchState{State: BatchIdle}
	s.revision++
	return nil
}

// queued is one file of a running batch.
type queued struct {
	id      string
	folder  string
	replace bool
}

func convertible(status string) bool {
	return status == StatusReady || status == StatusFailed || status == StatusCancelled
}

// Start converts every file that is ready. When output names already exist and no policy
// is given, nothing starts and the names are returned so the user can decide.
func (s *Service) Start(policy string) (StartResult, error) {
	result := StartResult{Conflicts: []Conflict{}}
	if policy != PolicyAsk && policy != PolicyKeep && policy != PolicyReplace {
		return result, errors.New("unknown choice for existing files")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != StateReady {
		return result, errors.New("Convert Me is not ready yet")
	}
	if s.busy() {
		return result, errors.New("a conversion is already running")
	}
	destination := ""
	if s.settings.DestinationMode == DestinationFolder {
		destination = s.settings.DestinationFolder
		if info, err := os.Stat(destination); err != nil || !info.IsDir() {
			return result, fmt.Errorf("the folder %s is no longer available, choose another one", destination)
		}
	}
	inputs := s.inputs()
	var queue []queued
	var reasons []string
	work := 0
	listed := nameSet{}
	for _, item := range s.items {
		if item.Status == StatusChecking {
			return result, errors.New("some files are still being checked, try again in a moment")
		}
		if item.Status == StatusSkipped {
			reasons = append(reasons, item.Error)
		}
		if !convertible(item.Status) {
			continue
		}
		target, _ := targetByID(item.Target)
		entry := queued{id: item.ID, folder: destination}
		if entry.folder == "" {
			entry.folder = item.Folder
		}
		if reason := skipReason(item.media, target); reason != "" {
			reasons = append(reasons, reason)
		} else {
			work++
			natural := filepath.Join(entry.folder, stem(item.Name)+target.Ext)
			if fileExists(natural) && !inputs.protects(natural) {
				entry.replace = policy == PolicyReplace
				if !listed[pathKey(natural)] {
					listed[pathKey(natural)] = true
					result.Conflicts = append(result.Conflicts, Conflict{
						ItemID: item.ID, Name: filepath.Base(natural), Folder: entry.folder,
					})
				}
			}
		}
		queue = append(queue, entry)
	}
	if work == 0 {
		if len(reasons) > 0 {
			return result, errors.New(nothingNeeded(reasons))
		}
		return result, errors.New("there is nothing to convert")
	}
	if len(result.Conflicts) > 0 && policy == PolicyAsk {
		return result, nil
	}
	result.Conflicts = []Conflict{}
	for _, entry := range queue {
		item := s.find(entry.id)
		item.Status = StatusQueued
		item.Progress, item.OutputPath, item.OutputName, item.OutputSize = 0, "", "", 0
		item.Error, item.Detail = "", ""
	}
	ctx, cancel := context.WithCancel(s.base)
	s.cancel = cancel
	s.done = make(chan struct{})
	s.batch = BatchState{State: BatchRunning, Total: len(queue)}
	s.revision++
	go s.runBatch(ctx, queue, inputs, s.done)
	result.Started = true
	return result, nil
}

// inputSet protects the original files. An output may never be saved over one of them.
type inputSet struct {
	keys  map[string]bool
	infos []os.FileInfo
}

func (s *Service) inputs() inputSet {
	set := inputSet{keys: make(map[string]bool, len(s.items))}
	for _, item := range s.items {
		set.keys[pathKey(item.Path)] = true
		if info, err := os.Stat(item.Path); err == nil {
			set.infos = append(set.infos, info)
		}
	}
	return set
}

// protects reports whether path is one of the originals, including the same file reached
// by a different spelling such as a short name or a link.
func (set inputSet) protects(path string) bool {
	if set.keys[pathKey(path)] {
		return true
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	for _, original := range set.infos {
		if os.SameFile(info, original) {
			return true
		}
	}
	return false
}

func (s *Service) runBatch(ctx context.Context, queue []queued, inputs inputSet, done chan struct{}) {
	defer close(done)
	taken := nameSet{}
	for _, entry := range queue {
		if ctx.Err() != nil {
			break
		}
		s.convertOne(ctx, entry, inputs, taken)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, entry := range queue {
		if item := s.find(entry.id); item != nil && item.Status == StatusQueued {
			item.Status = StatusCancelled
			s.batch.Cancelled++
		}
	}
	s.batch.State = BatchFinished
	s.batch.CurrentID = ""
	s.cancel()
	s.revision++
}

func (s *Service) convertOne(ctx context.Context, entry queued, inputs inputSet, taken nameSet) {
	s.mu.Lock()
	item := s.find(entry.id)
	if item == nil {
		s.mu.Unlock()
		return
	}
	target, _ := targetByID(item.Target)
	if reason := skipReason(item.media, target); reason != "" {
		item.Status = StatusSkipped
		item.Error = reason
		s.batch.Skipped++
		s.revision++
		s.mu.Unlock()
		return
	}
	media, input, base := item.media, item.Path, stem(item.Name)
	item.Status = StatusConverting
	s.batch.CurrentID = item.ID
	s.revision++
	s.mu.Unlock()

	workDir := filepath.Join(s.dataDir, "work", entry.id)
	temp := partialPath(entry.folder, base, target.Ext)
	var (
		outcome Outcome
		final   string
	)
	err := os.MkdirAll(workDir, 0700)
	if err == nil && media.system {
		// Windows reads the photo and writes a copy of the picture into the work folder.
		// The engine converts that copy, and the copy goes away with the folder.
		source := filepath.Join(workDir, "source.png")
		if _, _, err = s.system.Export(input, source, 0); err == nil {
			if err = ctx.Err(); err == nil {
				media, err = s.engine.Probe(ctx, source)
				input = source
			}
		}
	}
	if err == nil {
		outcome, err = s.engine.Convert(ctx, Job{
			Media: media, Target: target, Input: input, Output: temp, WorkDir: workDir,
		}, func(percent float64) {
			s.mu.Lock()
			if percent > item.Progress {
				item.Progress = percent
				s.revision++
			}
			s.mu.Unlock()
		})
	}
	// Only the exact per-file work folder created above is removed.
	_ = os.RemoveAll(workDir)
	if err == nil {
		final, err = place(temp, entry.folder, base, target.Ext, entry.replace, taken, inputs.protects)
	}
	if err != nil {
		_ = os.Remove(temp)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.revision++
	switch {
	case err == nil:
		item.Status = StatusDone
		item.Progress = 100
		item.OutputPath, item.OutputName = final, filepath.Base(final)
		if info, statErr := os.Stat(final); statErr == nil {
			item.OutputSize = info.Size()
		}
		if outcome.Warning != "" {
			item.Warning = strings.TrimSpace(item.Warning + " " + outcome.Warning)
		}
		s.batch.Done++
	case ctx.Err() != nil:
		item.Status = StatusCancelled
		item.Progress = 0
		s.batch.Cancelled++
	default:
		item.Status = StatusFailed
		item.Progress = 0
		item.Error, item.Detail = explain(err)
		s.batch.Failed++
	}
}

// Cancel stops the running batch. Finished files are kept, the unfinished one is removed.
func (s *Service) Cancel() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.batch.State != BatchRunning {
		return errors.New("nothing is being converted")
	}
	s.batch.State = BatchCancelling
	s.cancel()
	s.revision++
	return nil
}

// Busy reports whether a batch is running.
func (s *Service) Busy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.busy()
}

// Outputs lists the files written so far, grouped by folder in list order.
func (s *Service) Outputs() (folders []string, files map[string][]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	files = map[string][]string{}
	for _, item := range s.items {
		if item.Status != StatusDone || item.OutputPath == "" {
			continue
		}
		folder := filepath.Dir(item.OutputPath)
		if _, seen := files[folder]; !seen {
			folders = append(folders, folder)
		}
		files[folder] = append(files[folder], item.OutputPath)
	}
	return folders, files
}

// OutputPath returns the converted file for one item, or "".
func (s *Service) OutputPath(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if item := s.find(id); item != nil && item.Status == StatusDone {
		return item.OutputPath
	}
	return ""
}

// Close stops everything and waits for it. Partial files of a running conversion are removed.
func (s *Service) Close() {
	s.stop()
	s.mu.Lock()
	done := s.done
	s.mu.Unlock()
	if done != nil {
		<-done
	}
	s.background.Wait()
}
