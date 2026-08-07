package i18n

// Key names one piece of user-facing text. Keys are constants rather than
// strings so a typo is a compile error instead of a blank line on screen.
type Key int

const (
	invalid Key = iota

	// Verbatim carries text that is already the message, such as an agent's
	// own command line complaining in its own words.
	Verbatim

	// Dates and shared words.
	Today
	Yesterday
	EmptySession
	ImagesOne
	ImagesMany
	You
	SessionsWord
	SessionsWordOne
	ArchivedSessions
	ArchivedSessionsOne
	EmptySessions
	EmptySessionsOne
	OrphanSessions
	OrphanSessionsOne

	// The three operations, in the three forms a sentence needs them.
	OpArchive
	OpUnarchive
	OpDelete
	OpArchiveProgress
	OpUnarchiveProgress
	OpDeleteProgress
	OpArchiveDone
	OpUnarchiveDone
	OpDeleteDone

	// Key bindings, as the footer names them.
	BindingArchive
	BindingUnarchive
	BindingDelete
	BindingCopySessionID
	BindingCopyCwd
	BindingDeleteArchived
	BindingDeleteEmpty
	BindingDeleteOrphans
	BindingSelect
	BindingSelectMore
	BindingArchiveSelected
	BindingUnarchiveSelected
	BindingDeleteSelected
	BindingDanger
	BindingSearch
	BindingReload
	BindingHelp
	BindingQuit
	BindingFocus

	// The help screen.
	HelpTitle
	HelpClose
	HelpBrowse
	HelpSelect
	HelpTopBottom
	HelpFocus
	HelpSearch
	HelpSearchFields
	HelpSearchReverse
	HelpSearchMatch
	HelpManage
	HelpSelectToggle
	HelpCopySessionID
	HelpCopyCwd
	HelpDelete
	HelpArchive
	HelpUnarchive
	HelpDeleteArchived
	HelpDeleteOrphans
	HelpDeleteEmpty
	HelpDanger
	HelpOther
	HelpEscape
	HelpReload
	HelpOpen
	HelpQuit

	// Confirmation dialogs.
	Cancel
	Delete
	Enable
	DeleteIrreversible
	DeleteDescendantOne
	DeleteDescendantMany
	ConfirmDeleteTitle
	ConfirmBulkTitle
	ConfirmSelectionTitle
	ConfirmBulkButton
	ConfirmDangerTitle
	BulkCount
	BulkRemaining
	DangerLineOne
	DangerLineTwo
	CascadeOrphansOne
	CascadeOrphansMany
	OrphanNote

	// The banner and the session list.
	AppTitle
	NoSessionsHere
	BannerSessionsOne
	BannerSessionsMany
	BannerArchivedOne
	BannerArchivedMany
	BannerOrphansOne
	BannerOrphansMany
	BannerSelectedOne
	BannerSelectedMany
	BannerDanger

	// The conversation pane.
	NoConversation
	ArchivedMarker
	MessagesOne
	MessagesMany
	CwdUnknown
	ParentDeleted
	SpawnedBy
	SourceUnrecorded
	MessageTruncatedOne
	MessageTruncatedMany

	// Search.
	SearchEmptyList
	SearchNotStarted
	SearchNotFound
	SearchWrappedForward
	SearchWrappedBackward
	SearchStatus

	// Status line: what a key did, or why it did nothing.
	Reloaded
	BusyCannotQuit
	PreviousBusy
	ListingStale
	AlreadyArchived
	NotArchived
	NoCurrentSession
	NoArchived
	NoneOf
	NoOrphans
	SelectionCleared
	SelectedNone
	SelectedAllArchived
	SelectedNoneArchived
	DangerOn
	DangerOff
	CopyingSessionID
	CopySessionIDSuccess
	CopyingCwd
	CopyCwdSuccess
	MissingCLIBrowse
	MissingCLIModify
	UnexpectedError
	OperationProgress
	OperationProgressOne
	OperationTailOne
	OperationTailMany
	OperationDone
	SubagentOperationFailed
	BulkProgress
	BulkFailed
	BulkDone

	// The agent chooser.
	PickerTitle
	PickerChoose
	PickerHint
	PickerNone
	PickerUnavailable
	PickerLoading
	PickerUnreadable
	PickerIncomplete
	PickerCountOne
	PickerCountMany
	PickerBrowseOnly
	PickerAgentNone

	// What an agent's own command line reports.
	AgentCLIStartFailed
	AgentCLITimeout
	AgentCLIUnknownFailure
	SessionFileMissing
	SessionIDMismatch
	SessionPathUnsafe
	SidecarDeleteFailed
	SessionDeleteFailed
	OpenCodeHomeNotDataDir
	OpenCodeBadSessionID

	// The command line.
	CLIDescription
	CLIUsage
	CLIArguments
	CLIOptions
	CLIError
	CLIHelp
	CLIAgent
	CLIAgentChoices
	CLIUnknownOption
	CLIOptionNeedsValue
	CLIUnexpectedArgument
	CLIDirectory
	CLICodexHome
	CLIClaudeHome
	CLIOpenCodeHome
	CLIPiHome
	CLIVersion
	CLIHomeUnavailable

	// numKeys is the count of defined keys, for the catalogue completeness test.
	numKeys
)
