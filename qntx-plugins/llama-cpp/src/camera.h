#pragma once

#include <algorithm>
#include <cmath>
#include <cstring>

#define GLM_FORCE_DEPTH_ZERO_TO_ONE
#include <glm/glm.hpp>
#include <glm/gtc/matrix_transform.hpp>

// First-person perspective camera for flying through the particle nebula.
// Produces a column-major 4x4 MVP matrix for Metal vertex shaders.
// WASD moves relative to facing direction, smooth interpolation via fly_to + update(dt).
struct Camera {
    glm::vec3 position{0.0f, 0.0f, 2.0f};
    float yaw = 0.0f;       // radians, 0 = looking along -Z
    float pitch = 0.0f;     // radians, clamped to [-1.5, 1.5]

    float fov = 60.0f;        // vertical FOV in degrees
    float near_plane = 0.01f;
    float far_plane = 100.0f;

    // Smooth interpolation target
    glm::vec3 target_position{0.0f, 0.0f, 2.0f};
    float target_yaw = 0.0f;
    float target_pitch = 0.0f;
    float lerp_speed = 8.0f;
    bool tracking = false;

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

    // Move relative to facing direction.
    void move_relative(float fwd, float rt, float u) {
        target_position += forward() * fwd + right() * rt + glm::vec3(0.0f, u, 0.0f);
        tracking = false;
    }

    void rotate(float dyaw, float dpitch) {
        target_yaw += dyaw;
        target_pitch += dpitch;
        target_pitch = std::clamp(target_pitch, -1.5f, 1.5f);
        tracking = false;
    }

    // Fly the camera to a world-space position, looking at it.
    void fly_to(glm::vec3 target_pos) {
        glm::vec3 dir = target_pos - position;
        float dist = glm::length(dir);
        if (dist < 0.01f) return;

        target_position = target_pos - glm::normalize(dir) * std::min(0.3f, dist * 0.5f);

        glm::vec3 d = glm::normalize(target_pos - target_position);
        target_yaw = atan2f(d.x, -d.z);
        target_pitch = asinf(std::clamp(d.y, -1.0f, 1.0f));

        tracking = true;
    }

    // Tick interpolation toward targets. Call once per frame.
    void update(float dt) {
        float t = 1.0f - expf(-lerp_speed * dt);
        position = glm::mix(position, target_position, t);
        yaw = yaw + (target_yaw - yaw) * t;
        pitch = pitch + (target_pitch - pitch) * t;
        pitch = std::clamp(pitch, -1.5f, 1.5f);
    }

    // Apply input deltas from WebSocket cam: messages.
    // dx=strafe, dy=ascend, dz remapped from zoom to forward, dyaw/dpitch=rotation.
    void apply(float dx, float dy, float dz, float dyaw, float dpitch) {
        float fwd_step = (1.0f - dz) * 0.5f;
        move_relative(fwd_step, dx, dy);
        rotate(dyaw, dpitch);
    }

    void reset(float center_x = 0.0f, float center_y = 0.0f, float extent = 1.0f) {
        position = glm::vec3(center_x, center_y, extent * 2.0f);
        target_position = position;
        yaw = 0.0f; pitch = 0.0f;
        target_yaw = 0.0f; target_pitch = 0.0f;
        tracking = false;
    }

    glm::mat4 view_matrix() const {
        return glm::lookAtLH(position, position + forward(), glm::vec3(0.0f, 1.0f, 0.0f));
    }

    glm::mat4 projection_matrix(float aspect) const {
        return glm::perspectiveLH_ZO(glm::radians(fov), aspect, near_plane, far_plane);
    }

    void build_mvp(float* mvp, int width, int height,
                   float /*center_x*/, float /*center_y*/, float /*extent*/) const {
        float aspect = static_cast<float>(width) / static_cast<float>(height);
        glm::mat4 m = projection_matrix(aspect) * view_matrix();
        std::memcpy(mvp, &m[0][0], 16 * sizeof(float));
    }
};
