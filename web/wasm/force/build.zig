const std = @import("std");

pub fn build(b: *std.Build) void {
    // WASM target for browsers
    const target = b.resolveTargetQuery(.{
        .cpu_arch = .wasm32,
        .os_tag = .freestanding,
    });

    const optimize = b.standardOptimizeOption(.{});

    const lib = b.addExecutable(.{
        .name = "force",
        .root_source_file = b.path("force.zig"),
        .target = target,
        .optimize = optimize,
    });

    // WASM-specific settings
    lib.entry = .disabled; // No entry point, we export functions
    lib.rdynamic = true; // Export all pub/export symbols

    // Stack size for WASM
    lib.stack_size = 64 * 1024; // 64KB stack

    // Output to web directory for easy serving
    const install = b.addInstallArtifact(lib, .{
        .dest_dir = .{ .override = .{ .custom = "../dist" } },
    });

    b.getInstallStep().dependOn(&install.step);

    // Convenience step to build just the wasm
    const wasm_step = b.step("wasm", "Build WASM module");
    wasm_step.dependOn(&install.step);
}
