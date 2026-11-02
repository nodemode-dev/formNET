// The window shows the page the formnet server serves on 127.0.0.1, which Tauri treats as a remote origin: commands reach
// it only through a capability with a remote URL list, and an app command needs a generated "allow-<name>" permission.
const APP_COMMANDS: &[&str] = &["set_window_title", "app_ready", "text_edit", "ui_zoom"];

fn main() {
    tauri_build::try_build(
        tauri_build::Attributes::new().app_manifest(tauri_build::AppManifest::new().commands(APP_COMMANDS)),
    )
    .expect("failed to run the Tauri build step");
}
