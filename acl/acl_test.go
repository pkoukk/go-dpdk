package acl

import (
	"syscall"
	"testing"
	"unsafe"

	"github.com/pkoukk/go-dpdk/common"
	"github.com/pkoukk/go-dpdk/eal"
)

func assert(t testing.TB, expected bool, args ...interface{}) {
	if !expected {
		t.Helper()
		t.Fatal(args...)
	}
}

func createSlices(alloc common.Allocator, size, n int) [][]byte {
	p := alloc.Malloc(uintptr(size * n))
	ret := make([][]byte, n)
	for i := range ret {
		ret[i] = unsafe.Slice((*byte)(unsafe.Add(p, size*i)), size)
	}
	return ret
}

func ExampleCreate() {
	defs := []FieldDef{
		// number of field definitions
		// see Build method
	}

	ctx, err := Create(&Param{
		Name: "my_acl_context",

		// we must specify an exact amount of memory required to
		// contain a rule given the number of field definitions.
		RuleSize: RuleSize(len(defs)),

		// maximum number of rules in a context
		MaxRuleNum: 64,

		SocketID: -1,
	})
	if err != nil {
		// handle error
	}

	_ = ctx // ACL context
}

func ExampleContext_AddRules() {
	var ctx *Context

	err := ctx.AddRules([]Rule{
		{
			// rule #1
			Data: RuleData{CategoryMask: 3, Priority: 1, Userdata: 1},
			Fields: []Field{
				// 1 byte field with value 1 and bitmask 0xff
				{uint8(1), uint8(0xff)},

				// 1 byte field with value 2 and bitmask 0xff
				{uint8(2), uint8(0xff)},

				// 1 byte field with value 3 and bitmask 0xff
				{uint8(3), uint8(0xff)},

				// 2 byte field with value 0x0102 and '/8' mask.
				{uint16(0x0102), uint8(8)},
			},
		},
	})

	if err != nil {
		// handle error
	}
}

func ExampleContext_Build() {
	defs := []FieldDef{
		{
			Type:       FieldTypeBitmask,
			Size:       1, // mandatory one byte field
			Offset:     0, // at the buffer's top
			FieldIndex: 0, // index of the field
			InputIndex: 0, // group of four (4) consecutive bytes.
		}, {
			Type:       FieldTypeBitmask,
			Size:       1,
			Offset:     1,
			FieldIndex: 1,
			InputIndex: 1, // N.B. same group for these three fields
		}, {
			Type:       FieldTypeBitmask,
			Size:       1,
			Offset:     2,
			FieldIndex: 2,
			InputIndex: 1,
		}, {
			Type:       FieldTypeMask,
			Size:       2,
			Offset:     3,
			FieldIndex: 3,
			InputIndex: 1,
		},
	}

	cfg := &Config{
		// Number of categories
		Categories: 2,

		// Size of internal data structures in ACL context.
		// 0 is the default which doesn't impose a hard limit.
		MaxSize: 0x800000,

		// Specify fields definitions which define a valid rule for
		// ACL context.
		Defs: defs,
	}

	// assume we have a context.
	var ctx *Context

	if err := ctx.Build(cfg); err != nil {
		// handle error
	}
}

func initBuffer(buf, data []byte) unsafe.Pointer {
	copy(buf, data)
	return unsafe.Pointer(&buf[0])
}

func TestContext(t *testing.T) {
	eal.InitOnceSafe("test", 4)

	// configuration of fields:
	// 1 + 1 + 1 + 2 bytes
	// simple as fuck
	cfg := &Config{
		Categories: 2,
		MaxSize:    0x800000,
		Defs: []FieldDef{
			{
				Type:       FieldTypeBitmask,
				Size:       1,
				Offset:     0,
				FieldIndex: 0,
				InputIndex: 0,
			}, {
				Type:       FieldTypeBitmask,
				Size:       1,
				Offset:     1,
				FieldIndex: 1,
				InputIndex: 1,
			}, {
				Type:       FieldTypeBitmask,
				Size:       1,
				Offset:     2,
				FieldIndex: 2,
				InputIndex: 1,
			}, {
				Type:       FieldTypeMask,
				Size:       2,
				Offset:     3,
				FieldIndex: 3,
				InputIndex: 1,
			},
		},
	}

	p := &Param{
		Name:       "hello",
		RuleSize:   RuleSize(len(cfg.Defs)),
		MaxRuleNum: 64,
		SocketID:   -1,
	}

	ctx, err := Create(p)
	assert(t, err == nil, err)
	assert(t, ctx != nil)

	ctx.Dump()

	ctx.Reset()

	// test FindExisting
	otherCtx, err := FindExisting(p.Name)
	assert(t, err == nil)
	assert(t, ctx == otherCtx)

	_, err = FindExisting("some_shit")
	assert(t, err == syscall.ENOENT)

	// invalid rules, wrong number of fields
	err = ctx.AddRules([]Rule{
		{
			Data: RuleData{CategoryMask: 3, Priority: 1, Userdata: 1},
			Fields: []Field{
				{uint8(1), uint8(0xff)},
			},
		}, {
			Data: RuleData{CategoryMask: 3, Priority: 2, Userdata: 1},
			Fields: []Field{
				{uint8(5), uint8(0xff)},
			},
		},
	})
	assert(t, err == syscall.EINVAL, err)

	// correct rules
	err = ctx.AddRules([]Rule{
		{
			// rule #1
			Data: RuleData{CategoryMask: 3, Priority: 1, Userdata: 1},
			Fields: []Field{
				{uint8(1), uint8(0xff)},
				{uint8(2), uint8(0xff)},
				{uint8(3), uint8(0xff)},
				{uint16(0x0102), uint8(8)},
			},
		}, {
			// rule #2
			Data: RuleData{CategoryMask: 3, Priority: 2, Userdata: 2},
			Fields: []Field{
				{uint8(5), uint8(0xff)},
				{uint8(6), uint8(0xff)},
				{uint8(7), uint8(0xff)},
				{uint16(0x0203), uint8(8)},
			},
		},
	})
	assert(t, err == nil, err)

	err = ctx.Build(cfg)
	assert(t, err == nil, err)

	// make test data
	//
	alloc := common.NewAllocatorSession(&common.StdAlloc{})
	defer alloc.Flush()

	inputData := createSlices(alloc, 5, 10)
	results := make([]uint32, 10)

	err = ctx.Classify([]unsafe.Pointer{
		initBuffer(inputData[0], []byte{1, 2, 3, 1, 0}),  // rule #1
		initBuffer(inputData[1], []byte{5, 6, 7, 2, 0}),  // rule #2
		initBuffer(inputData[2], []byte{5, 6, 7, 2, 9}),  // rule #2
		initBuffer(inputData[3], []byte{5, 6, 7, 1, 0}),  // mismatch
		initBuffer(inputData[4], []byte{1, 3, 3, 1, 0}),  // mismatch
		initBuffer(inputData[5], []byte{1, 2, 3, 2, 0}),  // mismatch
		initBuffer(inputData[6], []byte{1, 2, 3, 1, 10}), // rule #1
	}, results, 1)
	assert(t, err == nil, err)

	assert(t, results[0] == 1, results) // rule #1 matches
	assert(t, results[1] == 2, results) // rule #2 matches
	assert(t, results[2] == 2, results) // rule #2 matches
	assert(t, results[3] == 0, results) // no rule matches
	assert(t, results[4] == 0, results) // no rule matches
	assert(t, results[5] == 0, results) // no rule matches
	assert(t, results[6] == 1, results) // rule #1 matches

	ListDump()
	ctx.ResetRules()
	ctx.Free()
}
