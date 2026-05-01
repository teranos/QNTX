// Main chat screen. Tokens stream in colored by confidence (green high,
// red low). Tap a token → fullscreen transition into the nebula view with
// that token's top-K alternatives fanned out in 3D PCA space.

import SwiftUI
import ScryKit

public struct ChatView: View {
    @StateObject var vm: ChatViewModel
    @Binding var modelURL: URL?

    public init(vm: ChatViewModel, modelURL: Binding<URL?>) {
        self._vm = StateObject(wrappedValue: vm)
        self._modelURL = modelURL
    }

    public var body: some View {
        ZStack {
            Color.black.ignoresSafeArea()
            VStack(spacing: 0) {
                statusBar
                ScrollViewReader { proxy in
                    ScrollView {
                        LazyVStack(alignment: .leading, spacing: 16) {
                            ForEach(vm.turns) { turn in
                                TurnView(turn: turn) { tokenIdx in
                                    vm.focusedTokenIndex = (turn.id, tokenIdx)
                                }
                            }
                        }
                        .padding()
                    }
                    .onChange(of: vm.turns.last?.signals.count) { _ in
                        if let last = vm.turns.last {
                            withAnimation { proxy.scrollTo(last.id, anchor: .bottom) }
                        }
                    }
                }
                inputBar
            }
        }
        .fullScreenCover(item: focusedBinding) { focus in
            NebulaScreen(vm: vm,
                          turnId: focus.turnId,
                          tokenIdx: focus.tokenIdx) {
                vm.focusedTokenIndex = nil
            }
        }
        .task(id: modelURL) {
            if let url = modelURL {
                await vm.loadModel(at: url)
            }
        }
    }

    private var statusBar: some View {
        HStack {
            Circle()
                .fill(statusColor)
                .frame(width: 8, height: 8)
            Text(statusText)
                .font(.caption)
                .foregroundStyle(.secondary)
            Spacer()
            if let name = vm.modelName {
                Text(name)
                    .font(.caption.monospaced())
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }
        }
        .padding(.horizontal)
        .padding(.vertical, 8)
        .background(.ultraThinMaterial)
    }

    private var statusColor: Color {
        switch vm.phase {
        case .ready: return .green
        case .generating: return .blue
        case .loading: return .yellow
        case .error: return .red
        case .unloaded: return .gray
        }
    }

    private var statusText: String {
        switch vm.phase {
        case .unloaded: return "no model"
        case .loading: return "loading…"
        case .ready: return "ready"
        case .generating: return "generating…"
        case .error(let msg): return msg
        }
    }

    private var inputBar: some View {
        HStack(spacing: 8) {
            TextField("ask anything", text: $vm.prompt, axis: .vertical)
                .textFieldStyle(.plain)
                .padding(10)
                .background(Color.white.opacity(0.06))
                .clipShape(RoundedRectangle(cornerRadius: 12))
            Button {
                Task { await vm.send() }
            } label: {
                Image(systemName: "arrow.up.circle.fill")
                    .font(.title2)
            }
            .disabled(vm.prompt.isEmpty || vm.phase == .generating || vm.phase == .loading)
        }
        .padding()
    }

    // Bridge the (UUID, Int) tuple into something Identifiable for fullScreenCover.
    private var focusedBinding: Binding<FocusedKey?> {
        Binding(
            get: {
                if let f = vm.focusedTokenIndex {
                    return FocusedKey(turnId: f.turnId, tokenIdx: f.tokenIdx)
                }
                return nil
            },
            set: { newValue in
                vm.focusedTokenIndex = newValue.map { ($0.turnId, $0.tokenIdx) }
            }
        )
    }
}

private struct FocusedKey: Identifiable, Hashable {
    let turnId: UUID
    let tokenIdx: Int
    var id: String { "\(turnId)-\(tokenIdx)" }
}

private struct TurnView: View {
    let turn: ChatViewModel.Turn
    let onTokenTap: (Int) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .top) {
                Text("›")
                    .foregroundStyle(.secondary)
                    .font(.body.monospaced())
                Text(turn.userText)
                    .foregroundStyle(.primary)
            }
            HStack(alignment: .top) {
                Text("·")
                    .foregroundStyle(.secondary)
                    .font(.body.monospaced())
                AssistantText(signals: turn.signals, onTokenTap: onTokenTap)
            }
        }
        .id(turn.id)
    }
}

private struct AssistantText: View {
    let signals: [TokenSignal]
    let onTokenTap: (Int) -> Void

    var body: some View {
        // Render token spans inline by composing Texts. Each span is a
        // tappable region with a color from confidence.
        FlowLayout(spacing: 0, lineSpacing: 4) {
            ForEach(Array(signals.enumerated()), id: \.element.id) { (idx, signal) in
                TokenSpan(signal: signal) {
                    onTokenTap(idx)
                }
            }
        }
    }
}

private struct TokenSpan: View {
    let signal: TokenSignal
    let onTap: () -> Void

    var body: some View {
        Text(signal.text)
            .foregroundStyle(color)
            .padding(.vertical, 1)
            .contentShape(Rectangle())
            .onTapGesture { onTap() }
    }

    // Confidence → color: low = red, high = green, mid = yellow. Eased so
    // the green band is wide (most tokens are confident).
    private var color: Color {
        let c = max(0, min(1, signal.confidence))
        let eased = pow(c, 0.5)
        return Color(hue: Double(eased) * 0.33, saturation: 0.7, brightness: 1.0)
    }
}

// Minimal flow layout — wraps token spans across lines without ellipsis.
// Per QNTX LAW: data is never hidden behind truncation.
private struct FlowLayout: Layout {
    var spacing: CGFloat
    var lineSpacing: CGFloat

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let width = proposal.width ?? .infinity
        var lines: [CGFloat] = [0]
        var lineHeight: CGFloat = 0
        var totalHeight: CGFloat = 0
        for v in subviews {
            let s = v.sizeThatFits(.unspecified)
            if lines.last! + s.width > width {
                totalHeight += lineHeight + lineSpacing
                lines.append(0)
                lineHeight = 0
            }
            lines[lines.count - 1] += s.width + spacing
            lineHeight = max(lineHeight, s.height)
        }
        totalHeight += lineHeight
        let maxLine = lines.max() ?? 0
        return CGSize(width: min(maxLine, width), height: totalHeight)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        var x = bounds.minX
        var y = bounds.minY
        var lineHeight: CGFloat = 0
        for v in subviews {
            let s = v.sizeThatFits(.unspecified)
            if x + s.width > bounds.maxX {
                x = bounds.minX
                y += lineHeight + lineSpacing
                lineHeight = 0
            }
            v.place(at: CGPoint(x: x, y: y), proposal: ProposedViewSize(s))
            x += s.width + spacing
            lineHeight = max(lineHeight, s.height)
        }
    }
}
