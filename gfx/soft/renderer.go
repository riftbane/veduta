// Package soft is the software rasterizer backend of Veduta.
//
// Pipeline: per draw command, vertices are transformed and lit (Lambert, per vertex) once,
// triangles are clipped in homogeneous clip space against all six frustum planes, snapped
// to 28.4 fixed point, culled, set up and binned into 64×64 screen tiles. Setup runs in
// parallel over contiguous chunks of the frame's triangle stream, each chunk with its own
// bins. Tiles are then rasterized in parallel by a persistent worker pool; each tile is
// owned by exactly one worker at a time and consumes the chunks in order, so triangles are
// processed in submission order and the output is identical for any number of workers.
//
// Rasterization uses integer edge functions with the top-left fill rule, perspective-
// correct attribute interpolation and a float32 depth buffer (less-than test). The steady
// state allocates nothing: every scratch buffer is reused across frames.
package soft

import (
	"errors"
	"fmt"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/riftbane/veduta/v2/gfx"
	"github.com/riftbane/veduta/v2/gmath"
)

// TileSize is the edge length of a screen tile in pixels.
const TileSize = 64

// Options configures a Renderer.
type Options struct {
	// Workers is the number of tile workers; 0 means runtime.GOMAXPROCS(0).
	// The rendered image does not depend on this value.
	Workers int
}

// Renderer is the software implementation of gfx.Backend. It is not safe for concurrent
// use; one goroutine drives it while it uses its own workers internally.
type Renderer struct {
	*handle
}

// handle is shared by every copy of a Renderer value; the cleanup that stops the workers
// is attached to it, so a live copy keeps the workers running.
type handle struct {
	*core
}

var _ gfx.Backend = (*Renderer)(nil)

// New creates a renderer. Call Close when done to stop its workers (an unreachable
// Renderer stops them automatically).
func New(opt Options) *Renderer {
	n := opt.Workers
	if n <= 0 {
		n = runtime.GOMAXPROCS(0)
	}
	c := &core{workers: n, chunks: make([]setupCtx, n)}
	h := &handle{c}
	r := &Renderer{h}
	if n > 1 {
		c.jobs = make(chan struct{})
		for i := 0; i < n; i++ {
			go c.worker()
		}
		// Methods that hand work to the workers keep the handle alive until they join.
		runtime.AddCleanup(h, (*core).stop, c)
	}
	return r
}

// Close stops the worker goroutines. Later calls to Begin and Draw return an error.
func (r *Renderer) Close() {
	r.closed = true
	r.stop()
}

// Phases run by the worker pool.
const (
	phaseRaster = iota
	phaseSetup
)

// core holds all renderer state; workers reference core, never Renderer, so an
// abandoned Renderer can be collected and its cleanup can stop the workers.
type core struct {
	workers  int
	jobs     chan struct{}
	wg       sync.WaitGroup
	stopOnce sync.Once
	closed   bool
	next     atomic.Int32
	phase    int // written before jobs are sent, read by workers after receiving

	textures []*texture
	meshes   []*gfx.MeshData

	target *gfx.Framebuffer
	began  bool

	// Per-Draw state.
	mode       gfx.RenderMode
	clear      bool
	clearColor uint32
	cmds       []cmdState
	xforms     []xform  // parallel to cmds
	srcs       []cmdSrc // parallel to cmds
	chunks     []setupCtx
	nchunks    int     // chunks used by the current Draw
	order      []int32 // tile indices in raster start order (orderTiles)
	weight     []int32 // triangles binned per tile (orderTiles)
	tilesX     int
	tilesY     int
	overdraw   []uint16
	stats      gfx.FrameStats
	fragments  atomic.Int64
	normals    bool       // interpolate normals (ModeNormals or a normal target)
	lightDir   gmath.Vec3 // normalized direction towards the light
}

func (c *core) stop() {
	if c.jobs != nil {
		c.stopOnce.Do(func() { close(c.jobs) })
	}
}

func (c *core) worker() {
	for range c.jobs {
		c.drain()
		c.wg.Done()
	}
}

// drain processes work items of the current phase until none are left.
func (c *core) drain() {
	if c.phase == phaseSetup {
		n := int32(c.nchunks)
		for {
			k := c.next.Add(1) - 1
			if k >= n {
				return
			}
			c.setupChunk(&c.chunks[k])
		}
	}
	n := int32(len(c.order))
	for {
		t := c.next.Add(1) - 1
		if t >= n {
			return
		}
		c.rasterTile(int(c.order[t]))
	}
}

// run executes phase on every worker (inline without workers) and waits for it.
func (c *core) run(phase int) {
	c.phase = phase
	c.next.Store(0)
	if c.jobs == nil {
		c.drain()
		return
	}
	c.wg.Add(c.workers)
	for i := 0; i < c.workers; i++ {
		c.jobs <- struct{}{}
	}
	c.wg.Wait()
}

// orderTiles lists the tiles with the most binned triangles first, so the longest tiles
// start early and the frame does not end waiting on one late heavy tile. Ties keep index
// order. Tiles are independent, so the order never changes the image.
func (c *core) orderTiles() {
	n := c.tilesX * c.tilesY
	if cap(c.order) < n {
		c.order = make([]int32, n)
		c.weight = make([]int32, n)
	}
	c.order, c.weight = c.order[:n], c.weight[:n]
	for i := range c.order {
		w := 0
		for k := 0; k < c.nchunks; k++ {
			w += len(c.chunks[k].bins[i])
		}
		c.order[i], c.weight[i] = int32(i), int32(w)
	}
	slices.SortFunc(c.order, func(a, b int32) int {
		if d := c.weight[b] - c.weight[a]; d != 0 {
			return int(d)
		}
		return int(a - b)
	})
}

// CreateTexture uploads a texture with its mip chain. Level dimensions must halve
// (rounding down, never below 1) from one level to the next.
func (r *Renderer) CreateTexture(t *gfx.TextureData) (gfx.TextureID, error) {
	if t == nil || len(t.Levels) == 0 {
		return 0, errors.New("soft: texture has no levels")
	}
	tex := &texture{wrap: t.Wrap}
	for i, l := range t.Levels {
		if l == nil || l.W <= 0 || l.H <= 0 || len(l.Pix) != l.W*l.H {
			return 0, fmt.Errorf("soft: texture level %d is malformed", i)
		}
		if i > 0 {
			p := t.Levels[i-1]
			if l.W != max(p.W/2, 1) || l.H != max(p.H/2, 1) {
				return 0, fmt.Errorf("soft: texture level %d is %dx%d, want %dx%d", i, l.W, l.H, max(p.W/2, 1), max(p.H/2, 1))
			}
		}
		tex.levels = append(tex.levels, newLevel(l))
	}
	tex.fast = t.Wrap == gfx.WrapRepeat
	for _, l := range tex.levels {
		tex.fast = tex.fast && l.wmask >= 0 && l.hmask >= 0
	}
	r.textures = append(r.textures, tex)
	return gfx.TextureID(len(r.textures)), nil
}

// CreateMesh registers mesh geometry. The renderer keeps a reference: the caller must not
// modify the mesh afterwards.
func (r *Renderer) CreateMesh(m *gfx.MeshData) (gfx.MeshID, error) {
	if m == nil {
		return 0, errors.New("soft: nil mesh")
	}
	if len(m.Indices)%3 != 0 {
		return 0, fmt.Errorf("soft: mesh index count %d is not a multiple of 3", len(m.Indices))
	}
	nv := uint32(len(m.Vertices))
	for i, idx := range m.Indices {
		if idx >= nv {
			return 0, fmt.Errorf("soft: mesh index %d = %d out of range (%d vertices)", i, idx, nv)
		}
	}
	r.meshes = append(r.meshes, m)
	return gfx.MeshID(len(r.meshes)), nil
}

// UpdateMesh replaces the geometry of an existing mesh handle.
func (r *Renderer) UpdateMesh(id gfx.MeshID, m *gfx.MeshData) error {
	if id < 1 || int(id) > len(r.meshes) {
		return fmt.Errorf("soft: unknown mesh %d", id)
	}
	if m == nil {
		return errors.New("soft: nil mesh")
	}
	if len(m.Indices)%3 != 0 {
		return fmt.Errorf("soft: mesh index count %d is not a multiple of 3", len(m.Indices))
	}
	nv := uint32(len(m.Vertices))
	for i, idx := range m.Indices {
		if idx >= nv {
			return fmt.Errorf("soft: mesh index %d = %d out of range (%d vertices)", i, idx, nv)
		}
	}
	r.meshes[id-1] = m
	return nil
}

// Begin starts rendering into target.
func (r *Renderer) Begin(target *gfx.Framebuffer) error {
	if r.closed {
		return errors.New("soft: renderer is closed")
	}
	if target == nil || target.W <= 0 || target.H <= 0 {
		return errors.New("soft: invalid target")
	}
	n := target.W * target.H
	if len(target.Color) != n || len(target.Depth) != n || len(target.ID) != n || (target.Normal != nil && len(target.Normal) != n) {
		return errors.New("soft: target buffers do not match its size")
	}
	r.target = target
	r.began = true
	r.tilesX, r.tilesY = (target.W+TileSize-1)/TileSize, (target.H+TileSize-1)/TileSize
	return nil
}

// End finishes the frame.
func (r *Renderer) End() error {
	if !r.began {
		return errors.New("soft: End without Begin")
	}
	r.began = false
	r.target = nil
	return nil
}

// Stats returns counters for the last Draw.
func (r *Renderer) Stats() gfx.FrameStats { return r.stats }

// Draw renders dl into the current target immediately.
func (r *Renderer) Draw(dl *gfx.DrawList) error {
	if r.closed {
		return errors.New("soft: renderer is closed")
	}
	if !r.began {
		return errors.New("soft: Draw without Begin")
	}
	defer runtime.KeepAlive(r.handle) // the workers must not be stopped mid-frame
	c := r.core
	c.stats = gfx.FrameStats{}
	c.fragments.Store(0)
	c.mode = dl.Mode
	c.clear = dl.Clear
	c.clearColor = dl.ClearColor
	if c.mode == gfx.ModeSilhouette || c.mode == gfx.ModeOverdraw || c.mode == gfx.ModeIDs || c.mode == gfx.ModeDepth {
		c.clearColor = 0xff000000
	}
	c.normals = c.mode == gfx.ModeNormals || c.target.Normal != nil
	c.lightDir = dl.Light.Dir.Neg().Normalize()
	if c.mode == gfx.ModeOverdraw {
		n := c.target.W * c.target.H
		if cap(c.overdraw) < n {
			c.overdraw = make([]uint16, n)
		}
		c.overdraw = c.overdraw[:n]
		if dl.Clear { // several draws per frame accumulate heat
			clear(c.overdraw)
		}
	}

	if err := c.setup(dl); err != nil {
		return err
	}
	c.rasterAll()
	c.stats.Fragments = int(c.fragments.Load())
	c.post(dl)
	c.drawLines(dl)
	return nil
}

// rasterAll processes every tile, in parallel when workers are available.
func (c *core) rasterAll() {
	c.orderTiles()
	c.run(phaseRaster)
}
