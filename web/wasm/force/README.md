# Force Simulation (Zig/WASM)

Force-directed graph layout implemented in Zig, compiled to WebAssembly.

Implements D3-compatible forces:
- **Link**: Spring-like attraction between connected nodes
- **Many-body**: Coulomb repulsion between all node pairs
- **Center**: Weak pull toward center point
- **Collision**: Prevent node overlap

## Building

### Option 1: Nix (recommended)

```bash
# Enter dev shell (includes Zig)
nix develop

# Build WASM
cd web/wasm/force
zig build -Doptimize=ReleaseFast
```

### Option 2: Direct Zig install

```bash
# Install Zig (macOS)
brew install zig

# Install Zig (Linux)
curl -LO https://ziglang.org/download/0.13.0/zig-linux-x86_64-0.13.0.tar.xz
tar xf zig-linux-x86_64-0.13.0.tar.xz
export PATH=$PWD/zig-linux-x86_64-0.13.0:$PATH

# Build WASM
cd web/wasm/force
zig build -Doptimize=ReleaseFast
```

### Option 3: Use make

```bash
make wasm-force
```

Output: `web/wasm/dist/force.wasm`

## Usage from TypeScript

```typescript
import { ForceSimulation } from './graph/force-wasm';

// Create simulation
const sim = await ForceSimulation.create(nodes, links);

// Configure
sim.center(width / 2, height / 2);
sim.charge(-2000);

// Render on tick
sim.on('tick', () => {
    const positions = sim.nodes();
    // Update DOM/Canvas with positions
});

// Start
sim.alpha(1).restart();

// Drag handling
sim.fix(nodeId, x, y);    // Pin node during drag
sim.unfix(nodeId);         // Release after drag
```

## Performance

Current implementation:
- Many-body: O(n²) - computes all pairs
- Collision: O(n²) - computes all pairs

For 1000 nodes, this is ~1M calculations per tick. Still faster than JS due to:
- No GC pauses
- Tight loops without allocation
- Float32 throughout (cache friendly)

Future optimization path:
- Barnes-Hut quadtree for O(n log n) many-body
- Spatial hash for O(n) average collision detection

## Memory Layout

Node (28 bytes):
```
offset 0:  x (f32)
offset 4:  y (f32)
offset 8:  vx (f32)
offset 12: vy (f32)
offset 16: fx (f32, NaN = not fixed)
offset 20: fy (f32, NaN = not fixed)
offset 24: radius (f32)
```

Link (16 bytes):
```
offset 0: source (u32, index)
offset 4: target (u32, index)
offset 8: distance (f32)
offset 12: strength (f32)
```
