package amule

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Status is the connection and transfer state of the core.
type Status struct {
	Version        string `json:"version"`
	Ed2kConnected  bool   `json:"ed2k_connected"`
	Ed2kConnecting bool   `json:"ed2k_connecting"`
	// HighID is true when the server assigned an ID above 0x00FFFFFF, i.e.
	// other clients can reach us directly.
	HighID        bool   `json:"high_id"`
	Ed2kID        uint32 `json:"ed2k_id"`
	ServerName    string `json:"server_name"`
	ServerAddress string `json:"server_address"`
	KadRunning    bool   `json:"kad_running"`
	KadConnected  bool   `json:"kad_connected"`
	KadFirewalled bool   `json:"kad_firewalled"`
	DownloadRate  int64  `json:"download_rate"`
	UploadRate    int64  `json:"upload_rate"`
	DownloadLimit int64  `json:"download_limit"`
	UploadLimit   int64  `json:"upload_limit"`
	UploadQueue   int64  `json:"upload_queue"`
	Ed2kUsers     int64  `json:"ed2k_users"`
	Ed2kFiles     int64  `json:"ed2k_files"`
	KadUsers      int64  `json:"kad_users"`
	KadFiles      int64  `json:"kad_files"`
	KadNodes      int64  `json:"kad_nodes"`
	// SharedFiles is how many files the core is sharing.
	SharedFiles int64 `json:"shared_files"`
	// TotalSent and TotalRecv are the bytes aMule has sent and received over
	// all its runs, the totals persisted in statistics.dat and shown in its
	// GUI next to the session figures.
	TotalSent int64 `json:"total_sent"`
	TotalRecv int64 `json:"total_recv"`
	// SessionSent and SessionRecv are the bytes aMule has sent and received
	// since it started, from the latest point of its own history graph.
	SessionSent int64 `json:"session_sent"`
	SessionRecv int64 `json:"session_recv"`
}

// Status asks the core for its statistics and connection state. The totals
// and the shared-file count travel only at DetailFull: aMule leaves them out
// of the command-level reply, where reading them as zero would be wrong.
func (c *Client) Status(ctx context.Context) (Status, error) {
	reply, err := c.Request(ctx, Packet{Op: OpStatReq, Tags: []Tag{IntTag(TagDetailLevel, DetailFull)}})
	if err != nil {
		return Status{}, err
	}
	status := Status{Version: c.ServerVersion()}
	value := func(name uint16) int64 {
		tag, ok := reply.Tag(name)
		if !ok {
			return 0
		}
		return int64(tag.Int())
	}
	status.DownloadRate = value(TagStatsDLSpeed)
	status.UploadRate = value(TagStatsULSpeed)
	status.DownloadLimit = value(TagStatsDLSpeedLimit)
	status.UploadLimit = value(TagStatsULSpeedLimit)
	status.UploadQueue = value(TagStatsULQueueLen)
	status.Ed2kUsers = value(TagStatsEd2kUsers)
	status.Ed2kFiles = value(TagStatsEd2kFiles)
	status.KadUsers = value(TagStatsKadUsers)
	status.KadFiles = value(TagStatsKadFiles)
	status.KadNodes = value(TagStatsKadNodes)
	status.SharedFiles = value(TagStatsSharedFiles)
	status.TotalSent = value(TagStatsTotalSent)
	status.TotalRecv = value(TagStatsTotalRecv)
	// The session totals live in a different reply (the history graph). They
	// are best effort: aMule without a sampled point still answers the status.
	if sent, received, totalsErr := c.SessionTotals(ctx); totalsErr == nil {
		status.SessionSent = sent
		status.SessionRecv = received
	}
	if conn, ok := reply.Tag(TagConnState); ok {
		applyConnState(&status, conn)
	}
	return status, nil
}

// SessionTotals returns the bytes the core has sent and received since it
// started. aMule reports them as KiB at the latest point of its history graph;
// one point is enough and keeps the reply small.
func (c *Client) SessionTotals(ctx context.Context) (sent, received int64, err error) {
	reply, err := c.Request(ctx, Packet{Op: OpGetStatsGraphs, Tags: []Tag{
		IntTag(TagDetailLevel, DetailWeb),
		IntTag(TagStatsGraphWidth, 1),
		IntTag(TagStatsGraphScale, 1),
	}})
	if err != nil {
		return 0, 0, err
	}
	number := func(name uint16) int64 {
		tag, ok := reply.Tag(name)
		if !ok {
			return 0
		}
		return int64(tag.Int()) * 1024
	}
	return number(TagStatsGraphSessionUL), number(TagStatsGraphSessionDL), nil
}

func applyConnState(status *Status, conn Tag) {
	bits := conn.Int()
	status.Ed2kConnected = bits&ConnEd2kConnected != 0
	status.Ed2kConnecting = bits&ConnEd2kConnecting != 0
	status.KadConnected = bits&ConnKadConnected != 0
	status.KadFirewalled = bits&ConnKadFirewalled != 0
	status.KadRunning = bits&ConnKadRunning != 0
	if id, ok := conn.ChildInt(TagEd2kID); ok && status.Ed2kConnected {
		status.Ed2kID = uint32(id)
		status.HighID = id >= 0x01000000
	}
	if server, ok := conn.Child(TagServer); ok {
		status.ServerAddress = ipv4Address(server.Data)
		status.ServerName = server.ChildString(TagServerName)
	}
}

// ipv4Address decodes EC_TAGTYPE_IPV4: four address bytes in dotted order
// followed by the port, big-endian.
func ipv4Address(data []byte) string {
	if len(data) != 6 {
		return ""
	}
	ip := net.IPv4(data[0], data[1], data[2], data[3])
	return net.JoinHostPort(ip.String(), strconv.Itoa(int(binary.BigEndian.Uint16(data[4:]))))
}

func ipv4Tag(name uint16, address string) (Tag, error) {
	host, portText, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil {
		return Tag{}, fmt.Errorf("amule: server address %q: %w", address, err)
	}
	ip := net.ParseIP(host).To4()
	port, portErr := strconv.Atoi(portText)
	if ip == nil || portErr != nil || port <= 0 || port > 65535 {
		return Tag{}, fmt.Errorf("amule: server address %q is not IPv4 host:port", address)
	}
	data := append([]byte(nil), ip...)
	data = binary.BigEndian.AppendUint16(data, uint16(port))
	return Tag{Name: name, Type: TypeIPv4, Data: data}, nil
}

// Server is one entry of the core's server list.
type Server struct {
	Name        string `json:"name"`
	Address     string `json:"address"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Users       int64  `json:"users"`
	MaxUsers    int64  `json:"max_users"`
	Files       int64  `json:"files"`
	Ping        int64  `json:"ping_ms"`
	Failed      int64  `json:"failed"`
	Static      bool   `json:"static"`
	Connected   bool   `json:"connected"`
}

// Servers returns the server list, the connected one marked, sorted by users.
func (c *Client) Servers(ctx context.Context) ([]Server, error) {
	reply, err := c.Request(ctx, Packet{Op: OpGetServerList, Tags: []Tag{IntTag(TagDetailLevel, DetailFull)}})
	if err != nil {
		return nil, err
	}
	current := ""
	if status, statusErr := c.connState(ctx); statusErr == nil {
		current = status.ServerAddress
	}
	servers := make([]Server, 0, len(reply.Tags))
	for _, tag := range reply.Tags {
		if tag.Name != TagServer {
			continue
		}
		address := ipv4Address(tag.Data)
		users, _ := tag.ChildInt(TagServerUsers)
		maxUsers, _ := tag.ChildInt(TagServerUsersMax)
		files, _ := tag.ChildInt(TagServerFiles)
		ping, _ := tag.ChildInt(TagServerPing)
		failed, _ := tag.ChildInt(TagServerFailed)
		static, _ := tag.ChildInt(TagServerStatic)
		servers = append(servers, Server{
			Name:        tag.ChildString(TagServerName),
			Address:     address,
			Description: tag.ChildString(TagServerDesc),
			Version:     tag.ChildString(TagServerVer),
			Users:       int64(users),
			MaxUsers:    int64(maxUsers),
			Files:       int64(files),
			Ping:        int64(ping),
			Failed:      int64(failed),
			Static:      static != 0,
			Connected:   address != "" && address == current,
		})
	}
	sort.SliceStable(servers, func(i, j int) bool { return servers[i].Users > servers[j].Users })
	return servers, nil
}

func (c *Client) connState(ctx context.Context) (Status, error) {
	reply, err := c.Request(ctx, Packet{Op: OpGetConnState, Tags: []Tag{IntTag(TagDetailLevel, DetailCmd)}})
	if err != nil {
		return Status{}, err
	}
	var status Status
	if conn, ok := reply.Tag(TagConnState); ok {
		applyConnState(&status, conn)
	}
	return status, nil
}

// Connect connects every enabled network (eD2k to any server, and Kad).
func (c *Client) Connect(ctx context.Context) error {
	_, err := c.requestOK(ctx, Packet{Op: OpConnect})
	return err
}

// Disconnect disconnects every network.
func (c *Client) Disconnect(ctx context.Context) error {
	_, err := c.requestOK(ctx, Packet{Op: OpDisconnect})
	return err
}

// ConnectServer connects to one server ("ip:port"), or to any server when
// address is empty.
func (c *Client) ConnectServer(ctx context.Context, address string) error {
	request := Packet{Op: OpServerConnect}
	if strings.TrimSpace(address) != "" {
		tag, err := ipv4Tag(TagServer, address)
		if err != nil {
			return err
		}
		request.Tags = []Tag{tag}
	}
	_, err := c.requestOK(ctx, request)
	return err
}

// DisconnectServer leaves the current eD2k server.
func (c *Client) DisconnectServer(ctx context.Context) error {
	_, err := c.requestOK(ctx, Packet{Op: OpServerDisconnect})
	return err
}

// UpdateServers asks the core to download a server.met from url.
func (c *Client) UpdateServers(ctx context.Context, url string) error {
	_, err := c.requestOK(ctx, Packet{Op: OpServerUpdateFromURL, Tags: []Tag{StringTag(TagString, strings.TrimSpace(url))}})
	return err
}

// UpdateNodes asks the core to download a Kad nodes.dat from url.
func (c *Client) UpdateNodes(ctx context.Context, url string) error {
	_, err := c.requestOK(ctx, Packet{Op: OpKadUpdateFromURL, Tags: []Tag{StringTag(TagString, strings.TrimSpace(url))}})
	return err
}

// StartKad and StopKad switch the Kad network.
func (c *Client) StartKad(ctx context.Context) error {
	_, err := c.requestOK(ctx, Packet{Op: OpKadStart})
	return err
}

func (c *Client) StopKad(ctx context.Context) error {
	_, err := c.requestOK(ctx, Packet{Op: OpKadStop})
	return err
}

// Download is one file of the core's download queue.
type Download struct {
	Hash        string `json:"hash"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	Done        int64  `json:"done"`
	Transferred int64  `json:"transferred"`
	// Uploaded is how many bytes of this file aMule has sent to other
	// clients: a file being downloaded is shared too, so it can have an
	// upload of its own.
	Uploaded int64  `json:"uploaded"`
	Speed    int64  `json:"speed"`
	Status   int    `json:"status"`
	Stopped  bool   `json:"stopped"`
	Sources  int64  `json:"sources"`
	Active   int64  `json:"active_sources"`
	Priority int    `json:"priority"`
	Category int    `json:"category"`
	Link     string `json:"link"`
	LastRecv int64  `json:"last_received"`
}

// Complete reports whether the core finished the file.
func (d Download) Complete() bool { return d.Status == StatusComplete }

// Paused reports whether the file is paused or stopped.
func (d Download) Paused() bool { return d.Status == StatusPaused || d.Stopped }

// Progress is the completed share in percent.
func (d Download) Progress() float64 {
	if d.Complete() {
		return 100
	}
	if d.Size <= 0 {
		return 0
	}
	value := float64(d.Done) * 100 / float64(d.Size)
	if value > 100 {
		value = 100
	}
	return value
}

// Downloads returns the download queue, completed files included until they
// are cleared.
func (c *Client) Downloads(ctx context.Context) ([]Download, error) {
	reply, err := c.Request(ctx, Packet{Op: OpGetDloadQueue, Tags: []Tag{IntTag(TagDetailLevel, DetailFull)}})
	if err != nil {
		return nil, err
	}
	out := make([]Download, 0, len(reply.Tags))
	for _, tag := range reply.Tags {
		if tag.Name != TagPartfile {
			continue
		}
		hashTag, _ := tag.Child(TagPartfileHash)
		hash, ok := hashTag.Hash()
		if !ok {
			continue
		}
		number := func(name uint16) int64 {
			value, _ := tag.ChildInt(name)
			return int64(value)
		}
		out = append(out, Download{
			Hash:        hex.EncodeToString(hash[:]),
			Name:        tag.ChildString(TagPartfileName),
			Size:        number(TagPartfileSizeFull),
			Done:        number(TagPartfileSizeDone),
			Transferred: number(TagPartfileSizeXfer),
			Uploaded:    number(TagPartfileSizeXferUp),
			Speed:       number(TagPartfileSpeed),
			Status:      int(number(TagPartfileStatus)),
			Stopped:     number(TagPartfileStopped) != 0,
			Sources:     number(TagPartfileSourceCount),
			Active:      number(TagPartfileSourceXfer),
			Priority:    int(number(TagPartfilePrio)),
			Category:    int(number(TagPartfileCat)),
			Link:        tag.ChildString(TagPartfileEd2kLink),
			LastRecv:    number(TagPartfileLastRecv),
		})
	}
	return out, nil
}

// SharedFile is one file aMule shares: its completed downloads among them.
type SharedFile struct {
	Hash string `json:"hash"`
	Name string `json:"name"`
	Size int64  `json:"size"`
	// Dir is the folder that holds the file on the core's filesystem. A file
	// still being downloaded carries its part-met name instead.
	Dir string `json:"dir"`
	// Upload counters of the shared file: bytes sent in this session and over
	// all time, requests answered, clients queued for it.
	Transferred    int64 `json:"transferred"`
	TransferredAll int64 `json:"transferred_all"`
	Requests       int64 `json:"requests"`
	RequestsAll    int64 `json:"requests_all"`
	Accepts        int64 `json:"accepts"`
	AcceptsAll     int64 `json:"accepts_all"`
	Queued         int64 `json:"queued"`
	// UploadRate is the bytes/s aMule is sending for this file right now
	// (aMule 3.x; older cores leave it at zero). LastUpload is the Unix time
	// of the last byte aMule uploaded for it, zero when it never did.
	UploadRate int64 `json:"upload_rate"`
	LastUpload int64 `json:"last_upload"`
	// Priority is the upload priority (PR_*); AutoPriority is set when the
	// core chooses it by itself (the reported value carries +10).
	Priority     int  `json:"priority"`
	AutoPriority bool `json:"auto_priority"`
}

// SharedFiles returns the files aMule shares. A finished download leaves the
// download queue and shows up here, in the incoming folder.
func (c *Client) SharedFiles(ctx context.Context) ([]SharedFile, error) {
	reply, err := c.Request(ctx, Packet{Op: OpGetSharedFiles, Tags: []Tag{IntTag(TagDetailLevel, DetailFull)}})
	if err != nil {
		return nil, err
	}
	out := make([]SharedFile, 0, len(reply.Tags))
	for _, tag := range reply.Tags {
		if tag.Name != TagKnownfile {
			continue
		}
		hashTag, _ := tag.Child(TagPartfileHash)
		hash, ok := hashTag.Hash()
		if !ok {
			continue
		}
		size, _ := tag.ChildInt(TagPartfileSizeFull)
		number := func(name uint16) int64 {
			child, _ := tag.ChildInt(name)
			return int64(child)
		}
		priority := int(number(TagKnownfilePrio))
		auto := false
		if priority >= 10 {
			priority -= 10
			auto = true
		}
		out = append(out, SharedFile{
			Hash:           hex.EncodeToString(hash[:]),
			Name:           tag.ChildString(TagPartfileName),
			Size:           int64(size),
			Dir:            tag.ChildString(TagKnownfileFilename),
			Transferred:    number(TagKnownfileXferred),
			TransferredAll: number(TagKnownfileXferredAll),
			Requests:       number(TagKnownfileReqCount),
			RequestsAll:    number(TagKnownfileReqAll),
			Accepts:        number(TagKnownfileAccepts),
			AcceptsAll:     number(TagKnownfileAcceptsAll),
			Queued:         number(TagKnownfileOnQueue),
			UploadRate:     number(TagKnownfileUploadRate),
			LastUpload:     number(TagKnownfileLastUpload),
			Priority:       priority,
			AutoPriority:   auto,
		})
	}
	return out, nil
}

// ReloadSharedFiles asks the core to rescan its shared folders, the same as
// the reload button in aMule: new files appear and removed ones disappear.
func (c *Client) ReloadSharedFiles(ctx context.Context) error {
	_, err := c.requestOK(ctx, Packet{Op: OpSharedFilesReload})
	return err
}

// UploadClient is one client aMule is uploading to or holding in its upload
// queue.
type UploadClient struct {
	Name        string `json:"name"`
	Software    string `json:"software"`
	IP          string `json:"ip"`
	State       int    `json:"state"`
	FileName    string `json:"file_name"`
	FileID      int64  `json:"file_id"`
	UpSpeed     int64  `json:"up_speed"`
	Uploaded    int64  `json:"uploaded"`
	UploadedAll int64  `json:"uploaded_all"`
	// WaitingPosition is the place in the upload queue (0 = not queued).
	WaitingPosition int64 `json:"waiting_position"`
}

// UploadQueue returns the clients in aMule's upload queue, the ones being
// uploaded to first.
func (c *Client) UploadQueue(ctx context.Context) ([]UploadClient, error) {
	reply, err := c.Request(ctx, Packet{Op: OpGetUloadQueue, Tags: []Tag{IntTag(TagDetailLevel, DetailFull)}})
	if err != nil {
		return nil, err
	}
	out := make([]UploadClient, 0, len(reply.Tags))
	for _, tag := range reply.Tags {
		if tag.Name != TagClient {
			continue
		}
		number := func(name uint16) int64 {
			child, _ := tag.ChildInt(name)
			return int64(child)
		}
		out = append(out, UploadClient{
			Name:            tag.ChildString(TagClientName),
			Software:        tag.ChildString(TagClientSoftVerStr),
			IP:              ipTag(tag),
			State:           int(number(TagClientUploadState)),
			FileName:        tag.ChildString(TagPartfileName),
			FileID:          number(TagClientUploadFile),
			UpSpeed:         number(TagClientUpSpeed),
			Uploaded:        number(TagClientUploadSession),
			UploadedAll:     number(TagClientUploadTotal),
			WaitingPosition: number(TagClientWaitingPos),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if (out[i].UpSpeed > 0) != (out[j].UpSpeed > 0) {
			return out[i].UpSpeed > 0
		}
		return out[i].WaitingPosition < out[j].WaitingPosition
	})
	return out, nil
}

// ipTag renders EC_TAG_CLIENT_USER_IP, whose payload is four bytes.
func ipTag(tag Tag) string {
	child, ok := tag.Child(TagClientUserIP)
	if !ok || len(child.Data) != 4 {
		return ""
	}
	return net.IPv4(child.Data[0], child.Data[1], child.Data[2], child.Data[3]).String()
}

// AddLink queues an ed2k:// link (file, server or server list).
func (c *Client) AddLink(ctx context.Context, link string, category int) error {
	tag := StringTag(TagString, strings.TrimSpace(link))
	if category > 0 {
		tag.Children = []Tag{IntTag(TagPartfileCat, uint64(category))}
	}
	_, err := c.requestOK(ctx, Packet{Op: OpAddLink, Tags: []Tag{tag}})
	return err
}

func hashArg(hash string) ([16]byte, error) {
	var out [16]byte
	raw, err := hex.DecodeString(strings.TrimSpace(hash))
	if err != nil || len(raw) != 16 {
		return out, fmt.Errorf("amule: %q is not an MD4 hash", hash)
	}
	copy(out[:], raw)
	return out, nil
}

func (c *Client) partfileCommand(ctx context.Context, op uint8, hash string, children ...Tag) error {
	value, err := hashArg(hash)
	if err != nil {
		return err
	}
	_, err = c.requestOK(ctx, Packet{Op: op, Tags: []Tag{HashTag(TagPartfile, value, children...)}})
	return err
}

// Pause, Resume and Delete act on one download by MD4 hash. Delete removes
// the partial data too, as in the aMule GUI.
func (c *Client) Pause(ctx context.Context, hash string) error {
	return c.partfileCommand(ctx, OpPartfilePause, hash)
}

func (c *Client) Resume(ctx context.Context, hash string) error {
	return c.partfileCommand(ctx, OpPartfileResume, hash)
}

func (c *Client) Delete(ctx context.Context, hash string) error {
	return c.partfileCommand(ctx, OpPartfileDelete, hash)
}

// Download priorities (PR_* in the core); a download reports its priority
// plus 10 while aMule chooses it automatically.
const (
	PriorityLow    = 0
	PriorityNormal = 1
	PriorityHigh   = 2
	PriorityAuto   = 5
)

// PriorityName names a download's reported priority: low, normal, high or
// auto.
func PriorityName(value int) string {
	if value >= 10 {
		return "auto"
	}
	switch value {
	case PriorityLow:
		return "low"
	case PriorityHigh:
		return "high"
	}
	return "normal"
}

// ParsePriority maps low/normal/high/auto to the core's value.
func ParsePriority(name string) (int, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "low":
		return PriorityLow, true
	case "normal":
		return PriorityNormal, true
	case "high":
		return PriorityHigh, true
	case "auto":
		return PriorityAuto, true
	}
	return 0, false
}

// SetPriority changes the download priority of a file.
func (c *Client) SetPriority(ctx context.Context, hash string, priority int) error {
	if priority < PriorityLow || (priority > PriorityHigh && priority != PriorityAuto) {
		return fmt.Errorf("amule: invalid priority %d", priority)
	}
	return c.partfileCommand(ctx, OpPartfilePrioSet, hash, IntTag(TagPartfilePrio, uint64(priority)))
}

// ClearCompleted removes the completed files from the download list (the
// files stay in the incoming directory).
func (c *Client) ClearCompleted(ctx context.Context) error {
	_, err := c.requestOK(ctx, Packet{Op: OpClearCompleted})
	return err
}

// SearchResult is one file found by a search.
type SearchResult struct {
	Hash            string `json:"hash"`
	Name            string `json:"name"`
	Size            int64  `json:"size"`
	Sources         int64  `json:"sources"`
	CompleteSources int64  `json:"complete_sources"`
	// Status is the core's knowledge of the file: 0 new, otherwise already
	// downloading, queued or known.
	Status int `json:"status"`
}

// Link is the ed2k:// link of the result.
func (r SearchResult) Link() string {
	name := strings.NewReplacer("|", "_", "\r", " ", "\n", " ").Replace(r.Name)
	return fmt.Sprintf("ed2k://|file|%s|%d|%s|/", name, r.Size, r.Hash)
}

// SearchOptions controls one search.
type SearchOptions struct {
	// Type is SearchKad, SearchGlobal or SearchLocal.
	Type    uint8
	MinSize uint64
	MaxSize uint64
	// Wait is how long to collect results. Kad searches report when they are
	// over; a server search is read until Wait elapses.
	Wait time.Duration
	// Poll is the interval between result reads (default 750 ms).
	Poll time.Duration
	// Retry is the pause before searching again when another client replaced
	// the result list (default 500 ms).
	Retry time.Duration
}

// ErrSearchRefused is the core declining a search (for example Kad not
// running or no eD2k server connected).
var ErrSearchRefused = errors.New("amule: search refused")

// ErrSearchBusy means another external client kept replacing aMule's result
// list while the search was running. aMule keeps one list shared by every EC
// client, so the only honest answer is to ask the caller to try again instead
// of returning another search's files.
var ErrSearchBusy = errors.New("amule: another client is searching")

// Search runs a search and collects the results. The core keeps a single
// result list, so a new search replaces the previous one: callers must not
// run searches concurrently on the same core. If another client (aMuleGUI, an
// external tool) starts one anyway, the search is retried a couple of times
// and then refused with ErrSearchBusy.
func (c *Client) Search(ctx context.Context, query string, options SearchOptions) ([]SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, errors.New("amule: empty search")
	}
	if options.Wait <= 0 {
		options.Wait = 20 * time.Second
	}
	if options.Retry <= 0 {
		options.Retry = 500 * time.Millisecond
	}
	if options.Poll <= 0 {
		options.Poll = 750 * time.Millisecond
	}
	search := IntTag(TagSearchType, uint64(options.Type),
		StringTag(TagSearchName, query),
		StringTag(TagSearchFileType, ""),
	)
	if options.MinSize > 0 {
		search.Children = append(search.Children, IntTag(TagSearchMinSize, options.MinSize))
	}
	if options.MaxSize > 0 {
		search.Children = append(search.Children, IntTag(TagSearchMaxSize, options.MaxSize))
	}
	// aMule keeps a single result list shared by every external client: if
	// another one (aMuleGUI, an external tool) starts a search, it wipes ours.
	// Detect that instead of returning its files as if they answered the query.
	const attempts = 3
	for attempt := 0; attempt < attempts; attempt++ {
		results, replaced, err := c.searchAttempt(ctx, query, search, options)
		if err != nil {
			return results, err
		}
		if !replaced {
			return results, nil
		}
		if attempt == attempts-1 {
			break
		}
		timer := time.NewTimer(options.Retry)
		select {
		case <-ctx.Done():
			// The caller gave up (or ran out of time) while waiting to retry:
			// report that, not a busy core.
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, ErrSearchBusy
}

// searchAttempt starts one search and polls its results. It reports replaced
// when the list shrinks or loses every hash it had, the signature of another
// client starting a search in the meantime; on the first poll, when there is
// nothing to compare with yet, a list where no file name carries any word of
// the query is taken as another search's.
func (c *Client) searchAttempt(ctx context.Context, query string, search Tag, options SearchOptions) ([]SearchResult, bool, error) {
	reply, err := c.Request(ctx, Packet{Op: OpSearchStart, Tags: []Tag{search}})
	if err != nil {
		return nil, false, err
	}
	if reply.Op == OpFailed {
		message := strings.TrimSpace(reply.Message())
		if message == "" {
			message = "unknown reason"
		}
		return nil, false, fmt.Errorf("%w: %s", ErrSearchRefused, message)
	}
	deadline := time.Now().Add(options.Wait)
	var results []SearchResult
	seen := map[string]struct{}{}
	stable := 0
	for {
		wait := options.Poll
		if remaining := time.Until(deadline); remaining < wait {
			wait = remaining
		}
		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return results, false, nil
			case <-timer.C:
			}
		}
		current, err := c.searchResults(ctx)
		if err != nil {
			return results, false, err
		}
		if searchListReplaced(seen, current) || (len(seen) == 0 && searchListForeign(query, current)) {
			return nil, true, nil
		}
		for _, result := range current {
			seen[result.Hash] = struct{}{}
		}
		if len(current) == len(results) {
			stable++
		} else {
			stable = 0
		}
		results = current
		if time.Now().After(deadline) {
			break
		}
		if options.Type == SearchKad && stable >= 1 && c.kadSearchFinished(ctx) {
			break
		}
	}
	_, _ = c.Request(ctx, Packet{Op: OpSearchStop})
	return results, false, nil
}

// searchListReplaced reports whether current no longer belongs to the search
// that produced seen: results only accumulate during a search, so a shorter
// list or one without a single known hash means another client started one.
func searchListReplaced(seen map[string]struct{}, current []SearchResult) bool {
	if len(seen) == 0 || len(current) == 0 {
		return false
	}
	if len(current) < len(seen) {
		return true
	}
	for _, result := range current {
		if _, ok := seen[result.Hash]; ok {
			return false
		}
	}
	return true
}

// searchListForeign reports whether no result names a single word of the
// query. aMule matches keywords against file names, so a list of answers to
// this query has at least one file carrying one of them. Only words of three
// or more letters count, compared without accents or separators; a query with
// none of them is never judged.
func searchListForeign(query string, current []SearchResult) bool {
	if len(current) == 0 {
		return false
	}
	words := strings.Fields(searchFoldText(query))
	terms := words[:0]
	for _, word := range words {
		if len(word) >= 3 {
			terms = append(terms, word)
		}
	}
	if len(terms) == 0 {
		return false
	}
	for _, result := range current {
		name := searchFoldText(result.Name)
		for _, term := range terms {
			if strings.Contains(name, term) {
				return false
			}
		}
	}
	return true
}

// searchFoldText lowercases text and keeps only ASCII letters and digits,
// turning everything else (accents, dots, dashes) into spaces, so "Città" and
// "citta" share the word "citt".
func searchFoldText(text string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		}
		return ' '
	}, text)
}

func (c *Client) kadSearchFinished(ctx context.Context) bool {
	reply, err := c.Request(ctx, Packet{Op: OpSearchProgress})
	if err != nil {
		return false
	}
	tag, ok := reply.Tag(TagSearchStatus)
	// The core reports 0xfffe once a Kad search is over.
	return ok && tag.Int() == 0xfffe
}

func (c *Client) searchResults(ctx context.Context) ([]SearchResult, error) {
	reply, err := c.Request(ctx, Packet{Op: OpSearchResults, Tags: []Tag{IntTag(TagDetailLevel, DetailFull)}})
	if err != nil {
		return nil, err
	}
	results := make([]SearchResult, 0, len(reply.Tags))
	for _, tag := range reply.Tags {
		if tag.Name != TagSearchFile {
			continue
		}
		hashTag, _ := tag.Child(TagPartfileHash)
		hash, ok := hashTag.Hash()
		if !ok {
			continue
		}
		size, _ := tag.ChildInt(TagPartfileSizeFull)
		sources, _ := tag.ChildInt(TagPartfileSourceCount)
		complete, _ := tag.ChildInt(TagPartfileSourceXfer)
		status, _ := tag.ChildInt(TagPartfileStatus)
		results = append(results, SearchResult{
			Hash:            hex.EncodeToString(hash[:]),
			Name:            tag.ChildString(TagPartfileName),
			Size:            int64(size),
			Sources:         int64(sources),
			CompleteSources: int64(complete),
			Status:          int(status),
		})
	}
	sort.SliceStable(results, func(i, j int) bool { return results[i].Sources > results[j].Sources })
	return results, nil
}

// Preferences are the core settings Gextto reads and changes.
type Preferences struct {
	IncomingDir string `json:"incoming_dir"`
	TempDir     string `json:"temp_dir"`
	// SharedDirs are the extra folders shared besides the incoming one.
	SharedDirs []string `json:"shared_dirs"`
	// AllocateFull is aMule's "allocate the full file up front" switch, the one
	// that stalls a NAS while it preallocates.
	AllocateFull bool `json:"allocate_full"`
	// MaxDownload and MaxUpload are in KiB/s; 0 means unlimited.
	MaxDownload int64 `json:"max_download_kib"`
	MaxUpload   int64 `json:"max_upload_kib"`
}

// Preferences reads the directories and the bandwidth limits.
func (c *Client) Preferences(ctx context.Context) (Preferences, error) {
	reply, err := c.Request(ctx, Packet{Op: OpGetPreferences, Tags: []Tag{
		IntTag(TagSelectPrefs, PrefsConnections|PrefsDirectories|PrefsFiles),
	}})
	if err != nil {
		return Preferences{}, err
	}
	var prefs Preferences
	if dirs, ok := reply.Tag(TagPrefsDirectories); ok {
		prefs.IncomingDir = dirs.ChildString(TagDirectoriesIncoming)
		prefs.TempDir = dirs.ChildString(TagDirectoriesTemp)
		if shared, ok := dirs.Child(TagDirectoriesShared); ok {
			for _, child := range shared.Children {
				if child.Type != TypeString {
					continue
				}
				if dir := strings.TrimSpace(child.String()); dir != "" {
					prefs.SharedDirs = append(prefs.SharedDirs, dir)
				}
			}
		}
	}
	if files, ok := reply.Tag(TagPrefsFiles); ok {
		_, prefs.AllocateFull = files.Child(TagFilesAllocFull)
	}
	if conn, ok := reply.Tag(TagPrefsConnections); ok {
		down, _ := conn.ChildInt(TagConnMaxDL)
		up, _ := conn.ChildInt(TagConnMaxUL)
		prefs.MaxDownload = int64(down)
		prefs.MaxUpload = int64(up)
	}
	return prefs, nil
}

// SetLimits changes the global bandwidth limits in KiB/s (0 = unlimited).
func (c *Client) SetLimits(ctx context.Context, downloadKiB, uploadKiB int64) error {
	if downloadKiB < 0 || uploadKiB < 0 {
		return errors.New("amule: negative bandwidth limit")
	}
	_, err := c.requestOK(ctx, Packet{Op: OpSetPreferences, Tags: []Tag{
		EmptyTag(TagPrefsConnections,
			IntTag(TagConnMaxDL, uint64(downloadKiB)),
			IntTag(TagConnMaxUL, uint64(uploadKiB)),
		),
	}})
	return err
}

// SetFilePreallocation switches aMule's full-file preallocation. It reads the
// current file preferences and sends them back with only that flag changed: a
// partial block would reset every other file option to false.
func (c *Client) SetFilePreallocation(ctx context.Context, enabled bool) error {
	reply, err := c.Request(ctx, Packet{Op: OpGetPreferences, Tags: []Tag{
		IntTag(TagSelectPrefs, PrefsFiles),
		IntTag(TagDetailLevel, DetailFull),
	}})
	if err != nil {
		return err
	}
	files, ok := reply.Tag(TagPrefsFiles)
	if !ok {
		return errors.New("amule: file preferences unavailable")
	}
	children := make([]Tag, 0, len(files.Children)+1)
	for _, child := range files.Children {
		if child.Name == TagFilesAllocFull {
			continue
		}
		children = append(children, child)
	}
	if enabled {
		children = append(children, EmptyTag(TagFilesAllocFull))
	}
	files.Children = children
	_, err = c.requestOK(ctx, Packet{Op: OpSetPreferences, Tags: []Tag{
		IntTag(TagDetailLevel, DetailFull),
		files,
	}})
	return err
}

// LogLines returns the core's log.
func (c *Client) LogLines(ctx context.Context) ([]string, error) {
	reply, err := c.Request(ctx, Packet{Op: OpGetLog})
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, tag := range reply.Tags {
		if tag.Name != TagString {
			continue
		}
		for _, line := range strings.Split(tag.String(), "\n") {
			if line = strings.TrimRight(line, "\r"); strings.TrimSpace(line) != "" {
				lines = append(lines, line)
			}
		}
	}
	return lines, nil
}
