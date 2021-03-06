// Package bdev contains bindings of SPDK block device layer.
package bdev

/*
#include "../../csrc/dpdk/bdev.h"
#include <spdk/thread.h>

extern void go_bdevEvent(enum spdk_bdev_event_type type, struct spdk_bdev* bdev, uintptr_t ctx);
extern void go_bdevIoComplete(struct spdk_bdev_io* io, bool success, uintptr_t ctx);

static int c_spdk_bdev_unmap_blocks(struct spdk_bdev_desc* desc, 	struct spdk_io_channel* ch, uint64_t offset_blocks, uint64_t num_blocks, uintptr_t ctx)
{
	return spdk_bdev_unmap_blocks(desc, ch, offset_blocks, num_blocks, (spdk_bdev_io_completion_cb)go_bdevIoComplete, (void*)ctx);
}
*/
import "C"
import (
	"errors"
	"runtime/cgo"
	"unsafe"

	"github.com/usnistgov/ndn-dpdk/core/cptr"
	"github.com/usnistgov/ndn-dpdk/core/logging"
	"github.com/usnistgov/ndn-dpdk/dpdk/eal"
	"github.com/usnistgov/ndn-dpdk/dpdk/pktmbuf"
	"go.uber.org/zap"
)

var logger = logging.New("bdev")

// Mode indicates mode of opening a block device.
type Mode bool

// Modes of opening a block device.
const (
	ReadOnly  Mode = false
	ReadWrite Mode = true
)

// Bdev represents an open block device descriptor.
type Bdev struct {
	logger    *zap.Logger
	c         *C.struct_spdk_bdev_desc
	ch        *C.struct_spdk_io_channel
	blockSize int64
	nBlocks   int64
}

// Open opens a block device.
func Open(device Device, mode Mode) (bd *Bdev, e error) {
	bdi := device.DevInfo()
	bd = &Bdev{
		logger: logger.With(zap.String("name", bdi.Name())),
	}
	eal.CallMain(func() {
		if res := C.spdk_bdev_open_ext(C.spdk_bdev_get_name(bdi.ptr()), C.bool(mode),
			C.spdk_bdev_event_cb_t(C.go_bdevEvent), nil, &bd.c); res != 0 {
			e = eal.MakeErrno(res)
			return
		}
		bd.ch = C.spdk_bdev_get_io_channel(bd.c)
	})
	if e != nil {
		return nil, e
	}
	bd.blockSize = int64(bdi.BlockSize())
	bd.nBlocks = int64(bdi.CountBlocks())
	bd.logger.Info("device opened",
		zap.Uintptr("ptr", uintptr(bd.Ptr())),
		zap.String("productName", bdi.ProductName()),
		zap.Int64("blockSize", bd.blockSize),
		zap.Int64("nBlocks", bd.nBlocks),
		zap.Reflect("driver", bdi.DriverInfo()),
		zap.Bool("canRead", bdi.HasIOType(IORead)),
		zap.Bool("canWrite", bdi.HasIOType(IOWrite)),
		zap.Bool("canUnmap", bdi.HasIOType(IOUnmap)),
	)
	return bd, nil
}

//export go_bdevEvent
func go_bdevEvent(typ C.enum_spdk_bdev_event_type, bdev *C.struct_spdk_bdev, ctx C.uintptr_t) {
	logger.Info("event",
		zap.Int("type", int(typ)),
		zap.Uintptr("bdev", uintptr(unsafe.Pointer(bdev))),
	)
}

// Close closes the block device.
func (bd *Bdev) Close() error {
	eal.CallMain(func() {
		C.spdk_put_io_channel(bd.ch)
		C.spdk_bdev_close(bd.c)
	})
	bd.logger.Info("device closed")
	return nil
}

// Ptr returns *C.struct_bdev_bdev_desc pointer.
func (bd *Bdev) Ptr() unsafe.Pointer {
	return unsafe.Pointer(bd.c)
}

// DevInfo returns Info about this device.
func (bd *Bdev) DevInfo() (bdi *Info) {
	return (*Info)(C.spdk_bdev_desc_get_bdev(bd.c))
}

// UnmapBlocks notifies the device that the data in the blocks are no longer needed.
func (bd *Bdev) UnmapBlocks(blockOffset, blockCount int64) error {
	done := make(chan error)
	ctx := cgo.NewHandle(done)
	defer ctx.Delete()
	eal.PostMain(cptr.Func0.Void(func() {
		res := C.c_spdk_bdev_unmap_blocks(bd.c, bd.ch, C.uint64_t(blockOffset), C.uint64_t(blockCount), C.uintptr_t(ctx))
		if res != 0 {
			done <- eal.MakeErrno(res)
		}
	}))
	return <-done
}

// ReadPacket reads blocks via scatter gather list.
func (bd *Bdev) ReadPacket(blockOffset, blockCount int64, pkt pktmbuf.Packet) error {
	done := make(chan error)
	ctx := cgo.NewHandle(done)
	defer ctx.Delete()
	eal.PostMain(cptr.Func0.Void(func() {
		res := C.SpdkBdev_ReadPacket(bd.c, bd.ch, (*C.struct_rte_mbuf)(pkt.Ptr()),
			C.uint64_t(blockOffset), C.uint64_t(blockCount), C.uint32_t(bd.blockSize),
			C.spdk_bdev_io_completion_cb(C.go_bdevIoComplete), C.uintptr_t(ctx))
		if res != 0 {
			done <- eal.MakeErrno(res)
		}
	}))
	return <-done
}

// WritePacket writes blocks via scatter gather list.
func (bd *Bdev) WritePacket(blockOffset, blockCount int64, pkt pktmbuf.Packet) error {
	done := make(chan error)
	ctx := cgo.NewHandle(done)
	defer ctx.Delete()
	eal.PostMain(cptr.Func0.Void(func() {
		res := C.SpdkBdev_WritePacket(bd.c, bd.ch, (*C.struct_rte_mbuf)(pkt.Ptr()),
			C.uint64_t(blockOffset), C.uint64_t(blockCount), C.uint32_t(bd.blockSize),
			C.spdk_bdev_io_completion_cb(C.go_bdevIoComplete), C.uintptr_t(ctx))
		if res != 0 {
			done <- eal.MakeErrno(res)
		}
	}))
	return <-done
}

//export go_bdevIoComplete
func go_bdevIoComplete(io *C.struct_spdk_bdev_io, success C.bool, ctx C.uintptr_t) {
	done := cgo.Handle(ctx).Value().(chan error)
	if bool(success) {
		done <- nil
	} else {
		done <- errors.New("bdev_io error")
	}

	C.spdk_bdev_free_io(io)
}
