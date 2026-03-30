#pragma once

#include <cmath>
#include <algorithm>

// Orthographic camera with pan, zoom, and 3D rotation (yaw/pitch).
// Produces a 4x4 column-major MVP matrix for Metal vertex shaders.
struct Camera {
    float x = 0, y = 0;            // pan offset (world space)
    float zoom = 1.0f;             // zoom multiplier
    float yaw = 0, pitch = 0;      // rotation (radians)

    void apply(float dx, float dy, float dz, float dyaw, float dpitch) {
        x += dx;
        y += dy;
        zoom *= dz;
        zoom = std::max(0.1f, std::min(50.0f, zoom));
        yaw += dyaw;
        pitch += dpitch;
        pitch = std::max(-1.5f, std::min(1.5f, pitch));
    }

    void reset() {
        x = y = 0;
        zoom = 1.0f;
        yaw = pitch = 0;
    }

    // Build column-major 4x4 MVP matrix.
    // center/extent define the auto-fit bounds from PCA.
    void build_mvp(float* mvp, int width, int height,
                   float center_x, float center_y, float extent) const {
        float base_scale = 0.9f / extent;
        float s = base_scale * zoom;
        float aspect = (float)width / (float)height;

        float cy = cosf(yaw), sy = sinf(yaw);
        float cp = cosf(pitch), sp = sinf(pitch);

        // R = Rx(pitch) * Ry(yaw)
        float r00 = cy,  r01 = sy*sp, r02 = sy*cp;
        float r10 = 0,   r11 = cp,    r12 = -sp;
        float r20 = -sy, r21 = cy*sp, r22 = cy*cp;

        float tx = center_x + x;
        float ty = center_y + y;

        float sx = s / aspect;
        // Z scale: compress into [0, 1] NDC range so rotation never clips.
        // Data spans ~2*extent after centering; map that to ~[0.1, 0.9].
        float sz = 0.4f / extent;

        mvp[0]  = sx * r00;  mvp[1]  = s * r10;  mvp[2]  = sz * r20; mvp[3]  = 0;
        mvp[4]  = sx * r01;  mvp[5]  = s * r11;  mvp[6]  = sz * r21; mvp[7]  = 0;
        mvp[8]  = sx * r02;  mvp[9]  = s * r12;  mvp[10] = sz * r22; mvp[11] = 0;
        mvp[12] = -(tx * mvp[0] + ty * mvp[4] + ty * mvp[8]);
        mvp[13] = -(tx * mvp[1] + ty * mvp[5] + ty * mvp[9]);
        mvp[14] = -(tx * mvp[2] + ty * mvp[6] + ty * mvp[10]) + 0.5f;
        mvp[15] = 1;
    }
};
