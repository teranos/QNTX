#pragma once

#include <algorithm>
#include <cmath>

#define GLM_FORCE_DEPTH_ZERO_TO_ONE
#include <glm/glm.hpp>
#include <glm/gtc/matrix_transform.hpp>

// Dual-mode camera: perspective (first-person) or orthographic (external observer).
// Produces a column-major 4x4 MVP matrix for Metal vertex shaders.
//
// Perspective mode: 3D eye position, yaw/pitch orientation, WASD moves relative
// to facing direction. Smooth interpolation via fly_to + update(dt).
//
// Orthographic mode: legacy pan/zoom behavior for external observation.
struct Camera {
    enum class Mode { Perspective, Orthographic };
    Mode mode = Mode::Perspective;

    // --- Perspective state ---
    glm::vec3 position{0.0f, 0.0f, 2.0f};  // eye position in world space
    float yaw = 0.0f;                        // radians, 0 = looking along -Z
    float pitch = 0.0f;                      // radians, clamped to [-1.5, 1.5]

    float fov = 60.0f;        // vertical FOV in degrees
    float near_plane = 0.01f;
    float far_plane = 100.0f;

    // Smooth interpolation target
    glm::vec3 target_position{0.0f, 0.0f, 2.0f};
    float target_yaw = 0.0f;
    float target_pitch = 0.0f;
    float lerp_speed = 8.0f;   // interpolation rate (higher = snappier)
    bool tracking = false;     // true when flying to a target

    // --- Orthographic state (legacy) ---
    float ortho_x = 0.0f, ortho_y = 0.0f;
    float ortho_zoom = 1.0f;

    // Forward and right vectors derived from yaw/pitch.
    glm::vec3 forward() const {
        return glm::normalize(glm::vec3(
            sinf(yaw) * cosf(pitch),
            sinf(pitch),
            -cosf(yaw) * cosf(pitch)
        ));
    }

    glm::vec3 right() const {
        return glm::normalize(glm::cross(forward(), glm::vec3(0.0f, 1.0f, 0.0f)));
    }

    glm::vec3 up() const {
        return glm::normalize(glm::cross(right(), forward()));
    }

    // Move relative to facing direction (perspective mode).
    // fwd = forward/backward, rt = strafe, u = ascend/descend.
    void move_relative(float fwd, float rt, float u) {
        target_position += forward() * fwd + right() * rt + glm::vec3(0.0f, u, 0.0f);
        tracking = false;  // manual movement breaks tracking
    }

    // Rotate the view.
    void rotate(float dyaw, float dpitch) {
        if (mode == Mode::Perspective) {
            target_yaw += dyaw;
            target_pitch += dpitch;
            target_pitch = std::clamp(target_pitch, -1.5f, 1.5f);
            tracking = false;
        } else {
            yaw += dyaw;
            pitch += dpitch;
            pitch = std::clamp(pitch, -1.5f, 1.5f);
        }
    }

    // Fly the camera to a world-space position, looking at it.
    void fly_to(glm::vec3 target_pos) {
        // Position the camera slightly offset from the target so it's visible
        glm::vec3 dir = target_pos - position;
        float dist = glm::length(dir);
        if (dist < 0.01f) return;

        // Target: look at the point from a small offset
        target_position = target_pos - glm::normalize(dir) * std::min(0.3f, dist * 0.5f);

        // Compute yaw/pitch to face the target
        glm::vec3 d = glm::normalize(target_pos - target_position);
        target_yaw = atan2f(d.x, -d.z);
        target_pitch = asinf(std::clamp(d.y, -1.0f, 1.0f));

        tracking = true;
    }

    // Tick interpolation toward targets. Call once per frame.
    void update(float dt) {
        if (mode != Mode::Perspective) return;

        float t = 1.0f - expf(-lerp_speed * dt);  // exponential ease
        position = glm::mix(position, target_position, t);
        yaw = yaw + (target_yaw - yaw) * t;
        pitch = pitch + (target_pitch - pitch) * t;
        pitch = std::clamp(pitch, -1.5f, 1.5f);
    }

    // Apply input deltas — unified entry point from WebSocket cam: messages.
    // In perspective mode: dx=strafe, dy=ascend, dz=forward (remapped from zoom),
    //                      dyaw/dpitch = rotation.
    // In ortho mode: dx/dy=pan, dz=zoom multiplier, dyaw/dpitch=rotation.
    void apply(float dx, float dy, float dz, float dyaw, float dpitch) {
        if (mode == Mode::Perspective) {
            // dz was multiplicative zoom (0.9 = in, 1.1 = out).
            // Remap: forward movement = (1 - dz) * scale.
            float fwd_step = (1.0f - dz) * 0.5f;
            move_relative(fwd_step, dx, dy);
            rotate(dyaw, dpitch);
        } else {
            ortho_x += dx;
            ortho_y += dy;
            ortho_zoom *= dz;
            ortho_zoom = std::clamp(ortho_zoom, 0.1f, 50.0f);
            yaw += dyaw;
            pitch += dpitch;
            pitch = std::clamp(pitch, -1.5f, 1.5f);
        }
    }

    void reset(float center_x = 0.0f, float center_y = 0.0f, float extent = 1.0f) {
        if (mode == Mode::Perspective) {
            // Position behind the center, looking at it
            position = glm::vec3(center_x, center_y, extent * 2.0f);
            target_position = position;
            yaw = 0.0f; pitch = 0.0f;
            target_yaw = 0.0f; target_pitch = 0.0f;
            tracking = false;
        } else {
            ortho_x = 0.0f; ortho_y = 0.0f;
            ortho_zoom = 1.0f;
            yaw = 0.0f; pitch = 0.0f;
        }
    }

    // View matrix (perspective mode).
    glm::mat4 view_matrix() const {
        glm::vec3 fwd = forward();
        return glm::lookAtLH(position, position + fwd, glm::vec3(0.0f, 1.0f, 0.0f));
    }

    // Projection matrix (perspective mode).
    glm::mat4 projection_matrix(float aspect) const {
        return glm::perspectiveLH_ZO(glm::radians(fov), aspect, near_plane, far_plane);
    }

    // Build column-major 4x4 MVP matrix.
    // In perspective mode: uses view + projection matrices.
    // In ortho mode: uses legacy pan/zoom/rotation (same as before).
    void build_mvp(float* mvp, int width, int height,
                   float center_x, float center_y, float extent) const {
        if (mode == Mode::Perspective) {
            float aspect = static_cast<float>(width) / static_cast<float>(height);
            glm::mat4 m = projection_matrix(aspect) * view_matrix();
            // GLM is column-major, Metal expects column-major — direct copy
            std::memcpy(mvp, &m[0][0], 16 * sizeof(float));
        } else {
            // Legacy orthographic path
            float base_scale = 0.9f / extent;
            float s = base_scale * ortho_zoom;
            float aspect = static_cast<float>(width) / static_cast<float>(height);

            float cy = cosf(yaw), sy = sinf(yaw);
            float cp = cosf(pitch), sp = sinf(pitch);

            float r00 = cy,  r01 = sy*sp, r02 = sy*cp;
            float r10 = 0,   r11 = cp,    r12 = -sp;
            float r20 = -sy, r21 = cy*sp, r22 = cy*cp;

            float tx = center_x + ortho_x;
            float ty = center_y + ortho_y;

            float sx = s / aspect;
            float sz = 0.4f / extent;

            mvp[0]  = sx * r00;  mvp[1]  = s * r10;  mvp[2]  = sz * r20; mvp[3]  = 0;
            mvp[4]  = sx * r01;  mvp[5]  = s * r11;  mvp[6]  = sz * r21; mvp[7]  = 0;
            mvp[8]  = sx * r02;  mvp[9]  = s * r12;  mvp[10] = sz * r22; mvp[11] = 0;
            mvp[12] = -(tx * mvp[0] + ty * mvp[4] + ty * mvp[8]);
            mvp[13] = -(tx * mvp[1] + ty * mvp[5] + ty * mvp[9]);
            mvp[14] = -(tx * mvp[2] + ty * mvp[6] + ty * mvp[10]) + 0.5f;
            mvp[15] = 1;
        }
    }
};
