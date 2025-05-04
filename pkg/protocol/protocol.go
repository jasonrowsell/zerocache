package protocol

// Command types
const (
	CmdSet uint8 = 1
	CmdGet uint8 = 2
	CmdDel uint8 = 3
)

// Response types
const (
	RespOK       uint8 = 1 // Generic OK
	RespError    uint8 = 2 // Error message follows
	RespValue    uint8 = 3 // Value data follows
	RespNotfound uint8 = 4 // Key not found (specific to GET)
)

// Size constants
const (
	MaxKeySize   = 1028      // 1KB limit for keys
	MaxValueSize = 64 * 1028 // 64KB limit for values

)
