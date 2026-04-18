#include "../src/camera.h"

#include <cassert>
#include <cmath>
#include <cstdio>

static bool near(float a, float b, float eps = 0.01f) {
    return fabsf(a - b) < eps;
}

static bool near_vec3(glm::vec3 a, glm::vec3 b, float eps = 0.01f) {
    return near(a.x, b.x, eps) && near(a.y, b.y, eps) && near(a.z, b.z, eps);
}

// forward() at yaw=0, pitch=0 should point along -Z
static void test_forward_default() {
    Camera cam;
    cam.yaw = 0; cam.pitch = 0;
    glm::vec3 f = cam.forward();
    assert(near(f.x, 0));
    assert(near(f.y, 0));
    assert(f.z < 0);  // -Z
    printf("  forward_default: OK\n");
}

// forward() at yaw=pi/2 should point along +X
static void test_forward_yaw90() {
    Camera cam;
    cam.yaw = M_PI / 2.0f; cam.pitch = 0;
    glm::vec3 f = cam.forward();
    assert(near(f.x, 1.0f));
    assert(near(f.y, 0));
    assert(near(f.z, 0));
    printf("  forward_yaw90: OK\n");
}

// forward() with pitch should tilt up/down
static void test_forward_pitch() {
    Camera cam;
    cam.yaw = 0; cam.pitch = M_PI / 4.0f;  // 45 degrees up
    glm::vec3 f = cam.forward();
    assert(f.y > 0.5f);  // pointing up
    assert(f.z < 0);     // still some -Z component
    printf("  forward_pitch: OK\n");
}

// move_relative with yaw=0: forward should move along -Z
static void test_move_relative_forward() {
    Camera cam;
    cam.position = glm::vec3(0);
    cam.target_position = glm::vec3(0);
    cam.yaw = 0; cam.pitch = 0;
    cam.target_yaw = 0; cam.target_pitch = 0;
    cam.move_relative(1.0f, 0, 0);  // 1 unit forward
    assert(cam.target_position.z < 0);  // moved along -Z
    assert(near(cam.target_position.x, 0));
    printf("  move_relative_forward: OK\n");
}

// move_relative with yaw=pi/2: forward should move along +X
static void test_move_relative_yaw90() {
    Camera cam;
    cam.position = glm::vec3(0);
    cam.target_position = glm::vec3(0);
    cam.yaw = M_PI / 2.0f; cam.pitch = 0;
    cam.target_yaw = cam.yaw; cam.target_pitch = 0;
    cam.move_relative(1.0f, 0, 0);
    assert(cam.target_position.x > 0.9f);  // moved along +X
    assert(near(cam.target_position.z, 0));
    printf("  move_relative_yaw90: OK\n");
}

// move_relative breaks tracking
static void test_move_breaks_tracking() {
    Camera cam;
    cam.tracking = true;
    cam.move_relative(0.1f, 0, 0);
    assert(!cam.tracking);
    printf("  move_breaks_tracking: OK\n");
}

// apply remaps dz to forward movement: dz=0.9 → forward, dz=1.1 → backward
static void test_apply_dz_remap() {
    Camera cam;
    cam.position = glm::vec3(0);
    cam.target_position = glm::vec3(0);
    cam.yaw = 0; cam.pitch = 0;
    cam.target_yaw = 0; cam.target_pitch = 0;

    cam.apply(0, 0, 0.9f, 0, 0);  // should move forward (along -Z)
    assert(cam.target_position.z < 0);

    cam.target_position = glm::vec3(0);
    cam.apply(0, 0, 1.1f, 0, 0);  // should move backward (along +Z)
    assert(cam.target_position.z > 0);
    printf("  apply_dz_remap: OK\n");
}

// apply dx = strafe
static void test_apply_strafe() {
    Camera cam;
    cam.position = glm::vec3(0);
    cam.target_position = glm::vec3(0);
    cam.yaw = 0; cam.pitch = 0;
    cam.target_yaw = 0; cam.target_pitch = 0;

    cam.apply(0.5f, 0, 1.0f, 0, 0);  // strafe right
    // With yaw=0, right vector should be along +X
    assert(cam.target_position.x > 0);
    printf("  apply_strafe: OK\n");
}

// rotate clamps pitch
static void test_rotate_pitch_clamp() {
    Camera cam;
    cam.target_pitch = 0;
    cam.rotate(0, 100.0f);  // way beyond limit
    assert(cam.target_pitch <= 1.5f);
    cam.rotate(0, -200.0f);
    assert(cam.target_pitch >= -1.5f);
    printf("  rotate_pitch_clamp: OK\n");
}

// fly_to sets target and enables tracking
static void test_fly_to() {
    Camera cam;
    cam.position = glm::vec3(0, 0, 2.0f);
    cam.target_position = cam.position;
    cam.yaw = 0; cam.pitch = 0;
    cam.target_yaw = 0; cam.target_pitch = 0;

    cam.fly_to(glm::vec3(1.0f, 0, 0));
    assert(cam.tracking);
    // target_position should be offset from (1,0,0), not exactly at it
    float dist = glm::length(cam.target_position - glm::vec3(1.0f, 0, 0));
    assert(dist > 0.01f && dist < 1.0f);
    printf("  fly_to: OK\n");
}

// update converges position toward target
static void test_update_convergence() {
    Camera cam;
    cam.position = glm::vec3(0);
    cam.target_position = glm::vec3(1.0f, 0, 0);
    cam.yaw = 0; cam.target_yaw = 1.0f;
    cam.pitch = 0; cam.target_pitch = 0;
    cam.lerp_speed = 8.0f;

    // Run many frames
    for (int i = 0; i < 300; i++) cam.update(1.0f / 60.0f);

    assert(near_vec3(cam.position, cam.target_position, 0.01f));
    assert(near(cam.yaw, cam.target_yaw, 0.01f));
    printf("  update_convergence: OK\n");
}

// reset puts camera at known state
static void test_reset() {
    Camera cam;
    cam.position = glm::vec3(99, 99, 99);
    cam.yaw = 3.0f; cam.pitch = 1.0f;
    cam.tracking = true;

    cam.reset(0.5f, 0.5f, 1.0f);
    assert(near_vec3(cam.position, glm::vec3(0.5f, 0.5f, 2.0f)));
    assert(near(cam.yaw, 0));
    assert(near(cam.pitch, 0));
    assert(!cam.tracking);
    printf("  reset: OK\n");
}

// build_mvp produces a valid matrix (non-zero, finite)
static void test_build_mvp() {
    Camera cam;
    cam.position = glm::vec3(0, 0, 2.0f);
    cam.yaw = 0; cam.pitch = 0;

    float mvp[16];
    cam.build_mvp(mvp, 800, 600, 0, 0, 1.0f);

    bool all_zero = true;
    for (int i = 0; i < 16; i++) {
        assert(std::isfinite(mvp[i]));
        if (mvp[i] != 0) all_zero = false;
    }
    assert(!all_zero);
    printf("  build_mvp: OK\n");
}

// Perspective depth: near plane maps to 0, far plane maps to 1 (Metal convention)
static void test_depth_range() {
    Camera cam;
    cam.position = glm::vec3(0, 0, 0);
    cam.yaw = 0; cam.pitch = 0;
    cam.fov = 60.0f;
    cam.near_plane = 0.01f;
    cam.far_plane = 100.0f;

    // Use the full MVP so the view matrix handles coordinate conventions.
    // Camera at origin looking along -Z (yaw=0), so a point at (0,0,-near)
    // is at the near plane in view space.
    float mvp_arr[16];
    cam.build_mvp(mvp_arr, 100, 100, 0, 0, 1.0f);
    glm::mat4 mvp_mat;
    std::memcpy(&mvp_mat[0][0], mvp_arr, 16 * sizeof(float));

    glm::vec4 near_pt = mvp_mat * glm::vec4(0, 0, -(cam.near_plane + 0.001f), 1.0f);
    float near_ndc_z = near_pt.z / near_pt.w;
    glm::vec4 far_pt = mvp_mat * glm::vec4(0, 0, -(cam.far_plane - 0.001f), 1.0f);
    float far_ndc_z = far_pt.z / far_pt.w;

    printf("    near_ndc_z=%.4f far_ndc_z=%.4f\n", near_ndc_z, far_ndc_z);
    // Metal depth [0,1]: near should be close to 0, far close to 1
    assert(near_ndc_z < far_ndc_z);  // depth increases with distance
    assert(near_ndc_z >= -0.05f && near_ndc_z <= 0.15f);  // near ~0
    assert(far_ndc_z >= 0.85f && far_ndc_z <= 1.05f);     // far ~1
    printf("  depth_range: OK\n");
}

int main() {
    printf("Camera tests:\n");
    test_forward_default();
    test_forward_yaw90();
    test_forward_pitch();
    test_move_relative_forward();
    test_move_relative_yaw90();
    test_move_breaks_tracking();
    test_apply_dz_remap();
    test_apply_strafe();
    test_rotate_pitch_clamp();
    test_fly_to();
    test_update_convergence();
    test_reset();
    test_build_mvp();
    test_depth_range();
    printf("All camera tests passed.\n");
    return 0;
}
