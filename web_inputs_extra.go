package gextto

// Additional web request structs defined after the input block of the web module.
// Field names follow the implementation contract (CamelCase by underscore segment);
// JSON tags keep the exact JSON names.

type MediaInfoQuery struct {
	Series  *string `json:"series"`
	Season  *int64  `json:"season"`
	Episode *int64  `json:"episode"`
	Movie   *string `json:"movie"`
	Year    *int64  `json:"year"`
}

type ProviderResetInput struct {
	Provider *string `json:"provider"`
}

type RenameInput struct {
	Force      bool `json:"force"`
	SourceOnly bool `json:"source_only"`
}

type SourceQuery struct {
	Q *string `json:"q"`
}

type TrashDeleteInput struct {
	Names []string `json:"names"`
	All   bool     `json:"all"`
}

type KeywordPruneInput struct {
	Keyword  string   `json:"keyword"`
	Keywords []string `json:"keywords"`
	Preview  bool     `json:"preview"`
}

type FtpTestInput struct {
	Host     *string `json:"host"`
	User     *string `json:"user"`
	Password *string `json:"password"`
	Path     *string `json:"path"`
}

type HandlerQuery struct {
	File string `json:"file"`
}

type BrowseQuery struct {
	Path *string `json:"path"`
}

type MkdirInput struct {
	Path string `json:"path"`
}

type DuplicatesInput struct {
	Execute bool `json:"execute"`
}

type BackfillMediaInfoInput struct {
	Limit *int `json:"limit"`
}

type TvdbQuery struct {
	Query string `json:"query"`
}

type FilePrioritiesInput struct {
	Priorities []int32 `json:"priorities"`
}

type TrackerEntryInput struct {
	Url  string `json:"url"`
	Tier int32  `json:"tier"`
}

type TrackersInput struct {
	Trackers []TrackerEntryInput `json:"trackers"`
}

type SuperSeedingInput struct {
	Enabled bool `json:"enabled"`
}

type ServiceActionInput struct {
	Action *string `json:"action"`
}

type WebSeedsInput struct {
	Urls   []string `json:"urls"`
	Remove bool     `json:"remove"`
}
