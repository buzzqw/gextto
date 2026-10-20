package amule

// Protocol constants of aMule's EC protocol (ECCodes for protocol 0x0204).
// Only the values this client uses are listed.

// ProtocolVersion is the EC protocol spoken by aMule 2.3.x.
const ProtocolVersion = 0x0204

// Opcodes.
const (
	OpNoop                = 0x01
	OpAuthReq             = 0x02
	OpAuthFail            = 0x03
	OpAuthOK              = 0x04
	OpFailed              = 0x05
	OpStrings             = 0x06
	OpMiscData            = 0x07
	OpAddLink             = 0x09
	OpStatReq             = 0x0A
	OpGetConnState        = 0x0B
	OpStats               = 0x0C
	OpGetDloadQueue       = 0x0D
	OpGetUloadQueue       = 0x0E
	OpGetSharedFiles      = 0x10
	OpPartfilePause       = 0x19
	OpPartfileResume      = 0x1A
	OpPartfilePrioSet     = 0x1C
	OpPartfileDelete      = 0x1D
	OpDloadQueue          = 0x1F
	OpUloadQueue          = 0x20
	OpSharedFiles         = 0x22
	OpSharedFilesReload   = 0x23
	OpSearchStart         = 0x26
	OpSearchStop          = 0x27
	OpSearchResults       = 0x28
	OpSearchProgress      = 0x29
	OpDownloadSearchRes   = 0x2A
	OpGetServerList       = 0x2C
	OpServerList          = 0x2D
	OpServerDisconnect    = 0x2E
	OpServerConnect       = 0x2F
	OpServerUpdateFromURL = 0x32
	OpGetLog              = 0x35
	OpLog                 = 0x38
	OpGetPreferences      = 0x3F
	OpSetPreferences      = 0x40
	OpKadStart            = 0x48
	OpKadStop             = 0x49
	OpConnect             = 0x4A
	OpDisconnect          = 0x4B
	OpKadUpdateFromURL    = 0x4D
	OpAuthSalt            = 0x4F
	OpAuthPasswd          = 0x50
	OpClearCompleted      = 0x53
)

// Tag names.
const (
	TagString          = 0x0000
	TagPasswdHash      = 0x0001
	TagProtocolVersion = 0x0002
	TagDetailLevel     = 0x0004
	TagConnState       = 0x0005
	TagEd2kID          = 0x0006
	TagClientID        = 0x000A
	TagPasswdSalt      = 0x000B
	TagKadID           = 0x0010
	TagClientName      = 0x0100
	TagClientVersion   = 0x0101

	TagStatsULSpeed      = 0x0200
	TagStatsDLSpeed      = 0x0201
	TagStatsULSpeedLimit = 0x0202
	TagStatsDLSpeedLimit = 0x0203
	TagStatsULQueueLen   = 0x0208
	TagStatsEd2kUsers    = 0x0209
	TagStatsKadUsers     = 0x020A
	TagStatsEd2kFiles    = 0x020B
	TagStatsKadFiles     = 0x020C
	TagStatsKadNodes     = 0x021B
	TagStatsSharedFiles  = 0x021A

	TagPartfile            = 0x0300
	TagPartfileName        = 0x0301
	TagPartfileSizeFull    = 0x0303
	TagPartfileSizeXfer    = 0x0304
	TagPartfileSizeDone    = 0x0306
	TagPartfileSpeed       = 0x0307
	TagPartfileStatus      = 0x0308
	TagPartfilePrio        = 0x0309
	TagPartfileSourceCount = 0x030A
	TagPartfileSourceXfer  = 0x030D
	TagPartfileEd2kLink    = 0x030E
	TagPartfileCat         = 0x030F
	TagPartfileLastRecv    = 0x0310
	TagPartfileStopped     = 0x0317
	TagPartfileHash        = 0x031E
	TagKnownfile           = 0x0400
	TagKnownfileXferred    = 0x0401
	TagKnownfileXferredAll = 0x0402
	TagKnownfileReqCount   = 0x0403
	TagKnownfileReqAll     = 0x0404
	TagKnownfileAccepts    = 0x0405
	TagKnownfileAcceptsAll = 0x0406
	TagKnownfileFilename   = 0x0408
	TagKnownfilePrio       = 0x040B
	TagKnownfileOnQueue    = 0x040C

	TagServer         = 0x0500
	TagServerName     = 0x0501
	TagServerDesc     = 0x0502
	TagServerAddress  = 0x0503
	TagServerPing     = 0x0504
	TagServerUsers    = 0x0505
	TagServerUsersMax = 0x0506
	TagServerFiles    = 0x0507
	TagServerPrio     = 0x0508
	TagServerFailed   = 0x0509
	TagServerStatic   = 0x050A
	TagServerVer      = 0x050B

	TagSearchFile         = 0x0700
	TagSearchType         = 0x0701
	TagSearchName         = 0x0702
	TagSearchMinSize      = 0x0703
	TagSearchMaxSize      = 0x0704
	TagSearchFileType     = 0x0705
	TagSearchExtension    = 0x0706
	TagSearchAvailability = 0x0707
	TagSearchStatus       = 0x0708

	TagClient              = 0x0600
	TagClientSoftware      = 0x0601
	TagClientUploadSession = 0x0609
	TagClientUploadTotal   = 0x060A
	TagClientUpSpeed       = 0x060D
	TagClientUserIP        = 0x0610
	TagClientSoftVerStr    = 0x0615
	TagClientWaitingPos    = 0x0616
	TagClientUploadState   = 0x061C
	TagClientUploadFile    = 0x061F

	TagSelectPrefs         = 0x1000
	TagPrefsConnections    = 0x1300
	TagConnMaxDL           = 0x1303
	TagConnMaxUL           = 0x1304
	TagPrefsDirectories    = 0x1A00
	TagDirectoriesIncoming = 0x1A01
	TagDirectoriesTemp     = 0x1A02
	TagDirectoriesShared   = 0x1A03
)

// Detail levels.
const (
	DetailCmd  = 0x00
	DetailWeb  = 0x01
	DetailFull = 0x02
)

// Search types.
const (
	SearchLocal  = 0x00
	SearchGlobal = 0x01
	SearchKad    = 0x02
)

// Preference selections.
const (
	PrefsConnections = 0x00000004
	PrefsDirectories = 0x00000200
)

// Part file states (PS_* in the core).
const (
	StatusReady        = 0
	StatusEmpty        = 1
	StatusWaitingHash  = 2
	StatusHashing      = 3
	StatusError        = 4
	StatusInsufficient = 5
	StatusUnknown      = 6
	StatusPaused       = 7
	StatusCompleting   = 8
	StatusComplete     = 9
	StatusAllocating   = 10
)

// Connection state bits of EC_TAG_CONNSTATE.
const (
	ConnEd2kConnected  = 0x01
	ConnEd2kConnecting = 0x02
	ConnKadConnected   = 0x04
	ConnKadFirewalled  = 0x08
	ConnKadRunning     = 0x10
)
