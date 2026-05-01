// Root view for the mobile-scry app. The host Xcode project provides the
// @main App and embeds ScryRootView — this lets us keep the SwiftPM target
// as a library while the Xcode shell remains thin.

import SwiftUI
import ScryKit

public struct ScryRootView: View {
    @State private var modelURL: URL?
    @StateObject private var chatVM = ChatViewModel()

    public init() {}

    public var body: some View {
        Group {
            if modelURL == nil {
                ModelPickerView(selectedModelURL: $modelURL)
            } else {
                ChatView(vm: chatVM, modelURL: $modelURL)
            }
        }
    }
}
