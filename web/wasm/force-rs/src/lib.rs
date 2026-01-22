// Force-directed graph simulation for WASM (Rust)
// Implements D3-compatible forces: link, many-body, center, collision
//
// Raw WASM exports (no wasm-bindgen) for fair comparison with Zig version.
// Uses no_std with static mut for WASM (single-threaded, no TLS needed).

#![no_std]

extern crate alloc;

use alloc::vec::Vec;

// Simple bump allocator for no_std WASM
mod allocator {
    use core::alloc::{GlobalAlloc, Layout};
    use core::cell::UnsafeCell;

    const HEAP_SIZE: usize = 1024 * 1024; // 1MB

    #[repr(C, align(16))]
    struct Heap {
        data: UnsafeCell<[u8; HEAP_SIZE]>,
        offset: UnsafeCell<usize>,
    }

    unsafe impl Sync for Heap {}

    static HEAP: Heap = Heap {
        data: UnsafeCell::new([0; HEAP_SIZE]),
        offset: UnsafeCell::new(0),
    };

    pub struct BumpAllocator;

    unsafe impl GlobalAlloc for BumpAllocator {
        unsafe fn alloc(&self, layout: Layout) -> *mut u8 {
            let offset = &mut *HEAP.offset.get();
            let align = layout.align();
            let size = layout.size();

            // Align up
            let aligned = (*offset + align - 1) & !(align - 1);
            let new_offset = aligned + size;

            if new_offset > HEAP_SIZE {
                core::ptr::null_mut()
            } else {
                *offset = new_offset;
                (*HEAP.data.get()).as_mut_ptr().add(aligned)
            }
        }

        unsafe fn dealloc(&self, _ptr: *mut u8, _layout: Layout) {
            // Bump allocator doesn't deallocate
        }
    }

    pub fn reset() {
        unsafe {
            *HEAP.offset.get() = 0;
        }
    }

    #[global_allocator]
    static ALLOCATOR: BumpAllocator = BumpAllocator;
}

#[panic_handler]
fn panic(_info: &core::panic::PanicInfo) -> ! {
    loop {}
}

// Fast square root using Newton-Raphson (no libm dependency)
#[inline]
fn sqrt_f32(x: f32) -> f32 {
    if x <= 0.0 {
        return 0.0;
    }
    // Initial guess using bit manipulation (fast inverse sqrt idea)
    let mut guess = x * 0.5;
    // 4 iterations of Newton-Raphson
    guess = 0.5 * (guess + x / guess);
    guess = 0.5 * (guess + x / guess);
    guess = 0.5 * (guess + x / guess);
    guess = 0.5 * (guess + x / guess);
    guess
}

#[inline]
fn max_f32(a: f32, b: f32) -> f32 {
    if a > b { a } else { b }
}

// Node state - matches D3's SimulationNodeDatum
#[repr(C)]
#[derive(Clone, Copy)]
pub struct Node {
    pub x: f32,
    pub y: f32,
    pub vx: f32,
    pub vy: f32,
    pub fx: f32, // NaN means not fixed
    pub fy: f32, // NaN means not fixed
    pub radius: f32,
}

impl Default for Node {
    fn default() -> Self {
        Self {
            x: 0.0,
            y: 0.0,
            vx: 0.0,
            vy: 0.0,
            fx: f32::NAN,
            fy: f32::NAN,
            radius: 60.0,
        }
    }
}

// Link between nodes
#[repr(C)]
#[derive(Clone, Copy)]
pub struct Link {
    pub source: u32,
    pub target: u32,
    pub distance: f32,
    pub strength: f32,
}

impl Default for Link {
    fn default() -> Self {
        Self {
            source: 0,
            target: 0,
            distance: 100.0,
            strength: 0.1,
        }
    }
}

// Force configuration
#[derive(Clone, Copy)]
struct Config {
    charge_strength: f32,
    charge_max_distance: f32,
    center_x: f32,
    center_y: f32,
    center_strength: f32,
    collision_strength: f32,
    velocity_decay: f32,
}

impl Default for Config {
    fn default() -> Self {
        Self {
            charge_strength: -2000.0,
            charge_max_distance: 400.0,
            center_x: 0.0,
            center_y: 0.0,
            center_strength: 0.05,
            collision_strength: 1.0,
            velocity_decay: 0.6,
        }
    }
}

// Global state (WASM is single-threaded, so static mut is safe)
static mut NODES: Vec<Node> = Vec::new();
static mut LINKS: Vec<Link> = Vec::new();
static mut CONFIG: Config = Config {
    charge_strength: -2000.0,
    charge_max_distance: 400.0,
    center_x: 0.0,
    center_y: 0.0,
    center_strength: 0.05,
    collision_strength: 1.0,
    velocity_decay: 0.6,
};
static mut RNG_STATE: u32 = 12345;

// ============================================================================
// WASM Exports
// ============================================================================

/// Initialize simulation with node and link counts
/// Returns pointer to node array for JS to populate
#[no_mangle]
pub extern "C" fn init(node_count: u32, link_count: u32) -> *mut Node {
    allocator::reset();

    unsafe {
        NODES = Vec::with_capacity(node_count as usize);
        LINKS = Vec::with_capacity(link_count as usize);
        CONFIG = Config::default();

        // Initialize nodes
        for _ in 0..node_count {
            NODES.push(Node::default());
        }

        // Initialize links
        for _ in 0..link_count {
            LINKS.push(Link::default());
        }

        NODES.as_mut_ptr()
    }
}

/// Get pointer to links array for JS to populate
#[no_mangle]
pub extern "C" fn getLinksPtr() -> *mut Link {
    unsafe { LINKS.as_mut_ptr() }
}

/// Set simulation center (call on resize)
#[no_mangle]
pub extern "C" fn setCenter(x: f32, y: f32) {
    unsafe {
        CONFIG.center_x = x;
        CONFIG.center_y = y;
    }
}

/// Set charge strength (for focus mode: -500, normal: -2000)
#[no_mangle]
pub extern "C" fn setChargeStrength(strength: f32) {
    unsafe {
        CONFIG.charge_strength = strength;
    }
}

/// Set collision radius for a node
#[no_mangle]
pub extern "C" fn setCollisionRadius(node_index: u32, radius: f32) {
    unsafe {
        if let Some(node) = NODES.get_mut(node_index as usize) {
            node.radius = radius;
        }
    }
}

/// Fix a node's position (for dragging)
#[no_mangle]
pub extern "C" fn fixNode(node_index: u32, x: f32, y: f32) {
    unsafe {
        if let Some(node) = NODES.get_mut(node_index as usize) {
            node.fx = x;
            node.fy = y;
        }
    }
}

/// Unfix a node's position
#[no_mangle]
pub extern "C" fn unfixNode(node_index: u32) {
    unsafe {
        if let Some(node) = NODES.get_mut(node_index as usize) {
            node.fx = f32::NAN;
            node.fy = f32::NAN;
        }
    }
}

/// Run one simulation tick
#[no_mangle]
pub extern "C" fn tick(alpha: f32) {
    unsafe {
        apply_link_force(alpha);
        apply_many_body_force(alpha);
        apply_center_force(alpha);
        apply_collision_force();
        apply_velocity();
    }
}

/// Get node count
#[no_mangle]
pub extern "C" fn getNodeCount() -> u32 {
    unsafe { NODES.len() as u32 }
}

/// Get link count
#[no_mangle]
pub extern "C" fn getLinkCount() -> u32 {
    unsafe { LINKS.len() as u32 }
}

// ============================================================================
// Force Implementations
// ============================================================================

/// Link force: spring-like attraction between connected nodes
unsafe fn apply_link_force(alpha: f32) {
    for i in 0..LINKS.len() {
        let link = LINKS[i];
        let source_idx = link.source as usize;
        let target_idx = link.target as usize;

        if source_idx >= NODES.len() || target_idx >= NODES.len() {
            continue;
        }

        let source_x = NODES[source_idx].x;
        let source_y = NODES[source_idx].y;
        let target_x = NODES[target_idx].x;
        let target_y = NODES[target_idx].y;

        let mut dx = target_x - source_x;
        let mut dy = target_y - source_y;

        // Handle coincident nodes
        if dx == 0.0 && dy == 0.0 {
            dx = jiggle();
            dy = jiggle();
        }

        let dist = sqrt_f32(dx * dx + dy * dy);
        let strength = link.strength * alpha;

        // Spring force
        let force = (dist - link.distance) / dist * strength;
        let fx = dx * force;
        let fy = dy * force;

        // Apply to both nodes
        NODES[target_idx].vx -= fx;
        NODES[target_idx].vy -= fy;
        NODES[source_idx].vx += fx;
        NODES[source_idx].vy += fy;
    }
}

/// Many-body force: repulsion between all pairs of nodes
/// O(n^2) - can be optimized with Barnes-Hut quadtree later
unsafe fn apply_many_body_force(alpha: f32) {
    let strength = CONFIG.charge_strength * alpha;
    let max_dist_sq = CONFIG.charge_max_distance * CONFIG.charge_max_distance;
    let n = NODES.len();

    for i in 0..n {
        for j in (i + 1)..n {
            let mut dx = NODES[j].x - NODES[i].x;
            let mut dy = NODES[j].y - NODES[i].y;

            // Handle coincident nodes
            if dx == 0.0 && dy == 0.0 {
                dx = jiggle();
                dy = jiggle();
            }

            let dist_sq = dx * dx + dy * dy;

            // Skip if beyond max distance
            if dist_sq > max_dist_sq {
                continue;
            }

            // Clamp to avoid extreme forces
            let clamped_dist_sq = max_f32(dist_sq, 1.0);
            let dist = sqrt_f32(clamped_dist_sq);

            // Coulomb's law: F = k / r^2
            let force = strength / clamped_dist_sq;
            let fx = dx / dist * force;
            let fy = dy / dist * force;

            // Apply to both nodes
            NODES[i].vx -= fx;
            NODES[i].vy -= fy;
            NODES[j].vx += fx;
            NODES[j].vy += fy;
        }
    }
}

/// Center force: weak pull toward center
unsafe fn apply_center_force(alpha: f32) {
    let strength = CONFIG.center_strength * alpha;
    let cx = CONFIG.center_x;
    let cy = CONFIG.center_y;

    for node in NODES.iter_mut() {
        node.vx += (cx - node.x) * strength;
        node.vy += (cy - node.y) * strength;
    }
}

/// Collision force: prevent overlap between nodes
/// O(n^2) - can be optimized with spatial index later
unsafe fn apply_collision_force() {
    let strength = CONFIG.collision_strength;
    let n = NODES.len();

    for i in 0..n {
        let ri = NODES[i].radius;

        for j in (i + 1)..n {
            let rj = NODES[j].radius;
            let min_dist = ri + rj;

            let mut dx = NODES[j].x - NODES[i].x;
            let mut dy = NODES[j].y - NODES[i].y;

            // Handle coincident nodes
            if dx == 0.0 && dy == 0.0 {
                dx = jiggle();
                dy = jiggle();
            }

            let dist_sq = dx * dx + dy * dy;
            let min_dist_sq = min_dist * min_dist;

            // Only apply if overlapping
            if dist_sq >= min_dist_sq {
                continue;
            }

            let dist = sqrt_f32(dist_sq);
            let overlap = min_dist - dist;

            // Push apart
            let push = overlap / dist * strength * 0.5;
            let px = dx * push;
            let py = dy * push;

            NODES[i].x -= px;
            NODES[i].y -= py;
            NODES[j].x += px;
            NODES[j].y += py;
        }
    }
}

/// Apply velocities to positions, handle fixed nodes
unsafe fn apply_velocity() {
    let decay = CONFIG.velocity_decay;

    for node in NODES.iter_mut() {
        if !node.fx.is_nan() {
            node.x = node.fx;
            node.vx = 0.0;
        } else {
            node.vx *= decay;
            node.x += node.vx;
        }

        if !node.fy.is_nan() {
            node.y = node.fy;
            node.vy = 0.0;
        } else {
            node.vy *= decay;
            node.y += node.vy;
        }
    }
}

// ============================================================================
// Utilities
// ============================================================================

/// Simple LCG random for jiggle (deterministic, no allocation)
unsafe fn jiggle() -> f32 {
    RNG_STATE = RNG_STATE.wrapping_mul(1103515245).wrapping_add(12345);
    ((RNG_STATE & 0xFFFF) as f32 / 65536.0 - 0.5) * 1e-6
}
