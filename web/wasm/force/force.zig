// Force-directed graph simulation for WASM
// Implements D3-compatible forces: link, many-body, center, collision
//
// This is a minimal implementation focused on correctness.
// Optimizations (quadtree for O(n log n) many-body/collision) come later.

const std = @import("std");

// Node state - matches D3's SimulationNodeDatum
pub const Node = struct {
    x: f32,
    y: f32,
    vx: f32 = 0,
    vy: f32 = 0,
    // Fixed position (NaN means not fixed)
    fx: f32 = std.math.nan(f32),
    fy: f32 = std.math.nan(f32),
    radius: f32 = 60, // Half of collision radius (120px / 2)
};

// Link between nodes
pub const Link = struct {
    source: u32,
    target: u32,
    distance: f32 = 100,
    strength: f32 = 0.1,
};

// Force configuration - mirrors GRAPH_PHYSICS from config.ts
pub const Config = struct {
    // Many-body (charge) force
    charge_strength: f32 = -2000,
    charge_max_distance: f32 = 400,

    // Center force
    center_x: f32 = 0,
    center_y: f32 = 0,
    center_strength: f32 = 0.05,

    // Collision force
    collision_strength: f32 = 1.0,

    // Velocity decay (friction)
    velocity_decay: f32 = 0.6,
};

// Simulation state
var nodes: []Node = &[_]Node{};
var links: []Link = &[_]Link{};
var config: Config = .{};

// Allocator for WASM (uses fixed buffer)
var buffer: [1024 * 1024]u8 = undefined; // 1MB buffer
var fba = std.heap.FixedBufferAllocator.init(&buffer);
const allocator = fba.allocator();

// ============================================================================
// WASM Exports
// ============================================================================

/// Initialize simulation with node and link counts
/// Returns pointer to node array for JS to populate
export fn init(node_count: u32, link_count: u32) [*]Node {
    // Reset allocator
    fba.reset();

    // Allocate nodes and links
    nodes = allocator.alloc(Node, node_count) catch &[_]Node{};
    links = allocator.alloc(Link, link_count) catch &[_]Link{};

    // Initialize nodes with default values
    for (nodes) |*node| {
        node.* = Node{
            .x = 0,
            .y = 0,
            .vx = 0,
            .vy = 0,
            .fx = std.math.nan(f32),
            .fy = std.math.nan(f32),
            .radius = 60,
        };
    }

    return nodes.ptr;
}

/// Get pointer to links array for JS to populate
export fn getLinksPtr() [*]Link {
    return links.ptr;
}

/// Set simulation center (call on resize)
export fn setCenter(x: f32, y: f32) void {
    config.center_x = x;
    config.center_y = y;
}

/// Set charge strength (for focus mode: -500, normal: -2000)
export fn setChargeStrength(strength: f32) void {
    config.charge_strength = strength;
}

/// Set collision radius multiplier (focus mode uses tighter collision)
export fn setCollisionRadius(node_index: u32, radius: f32) void {
    if (node_index < nodes.len) {
        nodes[node_index].radius = radius;
    }
}

/// Fix a node's position (for dragging)
export fn fixNode(node_index: u32, x: f32, y: f32) void {
    if (node_index < nodes.len) {
        nodes[node_index].fx = x;
        nodes[node_index].fy = y;
    }
}

/// Unfix a node's position
export fn unfixNode(node_index: u32) void {
    if (node_index < nodes.len) {
        nodes[node_index].fx = std.math.nan(f32);
        nodes[node_index].fy = std.math.nan(f32);
    }
}

/// Run one simulation tick
/// alpha: simulation temperature (0-1), controls force strength
export fn tick(alpha: f32) void {
    applyLinkForce(alpha);
    applyManyBodyForce(alpha);
    applyCenterForce(alpha);
    applyCollisionForce();
    applyVelocity(alpha);
}

/// Get node count
export fn getNodeCount() u32 {
    return @intCast(nodes.len);
}

/// Get link count
export fn getLinkCount() u32 {
    return @intCast(links.len);
}

// ============================================================================
// Force Implementations
// ============================================================================

/// Link force: spring-like attraction between connected nodes
fn applyLinkForce(alpha: f32) void {
    for (links) |link| {
        if (link.source >= nodes.len or link.target >= nodes.len) continue;

        const source = &nodes[link.source];
        const target = &nodes[link.target];

        var dx = target.x - source.x;
        var dy = target.y - source.y;

        // Handle coincident nodes
        if (dx == 0 and dy == 0) {
            dx = jiggle();
            dy = jiggle();
        }

        const dist = @sqrt(dx * dx + dy * dy);
        const strength = link.strength * alpha;

        // Spring force: F = k * (distance - rest_length)
        const force = (dist - link.distance) / dist * strength;

        const fx = dx * force;
        const fy = dy * force;

        // Apply to both nodes (Newton's third law)
        target.vx -= fx;
        target.vy -= fy;
        source.vx += fx;
        source.vy += fy;
    }
}

/// Many-body force: repulsion between all pairs of nodes
/// O(n^2) - can be optimized with Barnes-Hut quadtree later
fn applyManyBodyForce(alpha: f32) void {
    const strength = config.charge_strength * alpha;
    const max_dist_sq = config.charge_max_distance * config.charge_max_distance;

    for (nodes, 0..) |*node_i, i| {
        for (nodes, 0..) |*node_j, j| {
            if (i >= j) continue; // Only compute each pair once

            var dx = node_j.x - node_i.x;
            var dy = node_j.y - node_i.y;

            // Handle coincident nodes
            if (dx == 0 and dy == 0) {
                dx = jiggle();
                dy = jiggle();
            }

            const dist_sq = dx * dx + dy * dy;

            // Skip if beyond max distance
            if (dist_sq > max_dist_sq) continue;

            // Avoid division by zero and extreme forces at very close range
            const clamped_dist_sq = @max(dist_sq, 1.0);
            const dist = @sqrt(clamped_dist_sq);

            // Coulomb's law: F = k / r^2
            const force = strength / clamped_dist_sq;

            const fx = dx / dist * force;
            const fy = dy / dist * force;

            // Apply to both nodes
            node_i.vx -= fx;
            node_i.vy -= fy;
            node_j.vx += fx;
            node_j.vy += fy;
        }
    }
}

/// Center force: weak pull toward center
fn applyCenterForce(alpha: f32) void {
    const strength = config.center_strength * alpha;

    for (nodes) |*node| {
        node.vx += (config.center_x - node.x) * strength;
        node.vy += (config.center_y - node.y) * strength;
    }
}

/// Collision force: prevent overlap between nodes
/// O(n^2) - can be optimized with spatial index later
fn applyCollisionForce() void {
    const strength = config.collision_strength;

    for (nodes, 0..) |*node_i, i| {
        const ri = node_i.radius;

        for (nodes, 0..) |*node_j, j| {
            if (i >= j) continue;

            const rj = node_j.radius;
            const min_dist = ri + rj;

            var dx = node_j.x - node_i.x;
            var dy = node_j.y - node_i.y;

            // Handle coincident nodes
            if (dx == 0 and dy == 0) {
                dx = jiggle();
                dy = jiggle();
            }

            const dist_sq = dx * dx + dy * dy;
            const min_dist_sq = min_dist * min_dist;

            // Only apply if overlapping
            if (dist_sq >= min_dist_sq) continue;

            const dist = @sqrt(dist_sq);
            const overlap = min_dist - dist;

            // Push apart proportionally
            const push = overlap / dist * strength * 0.5;
            const px = dx * push;
            const py = dy * push;

            node_i.x -= px;
            node_i.y -= py;
            node_j.x += px;
            node_j.y += py;
        }
    }
}

/// Apply velocities to positions, handle fixed nodes
fn applyVelocity(alpha: f32) void {
    _ = alpha;

    for (nodes) |*node| {
        // If node is fixed, snap to fixed position
        if (!std.math.isNan(node.fx)) {
            node.x = node.fx;
            node.vx = 0;
        } else {
            node.vx *= config.velocity_decay;
            node.x += node.vx;
        }

        if (!std.math.isNan(node.fy)) {
            node.y = node.fy;
            node.vy = 0;
        } else {
            node.vy *= config.velocity_decay;
            node.y += node.vy;
        }
    }
}

// ============================================================================
// Utilities
// ============================================================================

// Simple LCG random for jiggle (deterministic, no allocation)
var rng_state: u32 = 12345;

fn jiggle() f32 {
    rng_state = rng_state *% 1103515245 +% 12345;
    // Small random value to break symmetry
    return (@as(f32, @floatFromInt(rng_state & 0xFFFF)) / 65536.0 - 0.5) * 1e-6;
}
