#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

mod reveal;
mod text_edit;
mod traffic_lights;
mod zoom;

use std::path::PathBuf;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use tauri::webview::{DownloadEvent, NewWindowResponse};
use tauri::{AppHandle, Manager, RunEvent, Url, WebviewUrl, WebviewWindowBuilder, WindowEvent};
use tauri_plugin_dialog::DialogExt;
use tauri_plugin_opener::OpenerExt;
use tauri_plugin_shell::process::{CommandChild, CommandEvent};
use tauri_plugin_shell::ShellExt;

const READY: &str = "formnet desktop ready http://127.0.0.1:";
const PREFERRED_ADDR: &str = "127.0.0.1:8760";
const FALLBACK_LOGICAL: (f64, f64) = (1280.0, 840.0);
const MIN_LOGICAL: (f64, f64) = (980.0, 640.0);
const SCREEN_FRACTION: f64 = 0.9;
const STOP_WAIT: Duration = Duration::from_secs(8);

struct Server {
    child: Mutex<Option<CommandChild>>,
    stopped: Arc<AtomicBool>,
    quitting: AtomicBool,
}

fn desktop_token() -> String {
    let mut bytes = [0u8; 32];
    getrandom::fill(&mut bytes).expect("the system random source failed");
    bytes.iter().map(|b| format!("{b:02x}")).collect()
}

fn data_dir(handle: &AppHandle) -> PathBuf {
    if let Some(dir) = std::env::var_os("FORMNET_DATA").filter(|v| !v.is_empty()) {
        return PathBuf::from(dir);
    }
    let home = handle
        .path()
        .home_dir()
        .unwrap_or_else(|_| PathBuf::from("."));
    home.join("formnet")
}

fn is_local(url: &Url, port: u16) -> bool {
    matches!(url.scheme(), "http" | "ws")
        && matches!(url.host_str(), Some("127.0.0.1") | Some("localhost"))
        && url.port() == Some(port)
}

fn open_outside(handle: &AppHandle, url: &Url) {
    if matches!(url.scheme(), "http" | "https" | "mailto") {
        let _ = handle.opener().open_url(url.as_str(), None::<&str>);
    }
}

fn file_name(url: &Url) -> String {
    url.path_segments()
        .and_then(|mut s| s.next_back().map(|v| v.to_string()))
        .filter(|v| !v.is_empty())
        .unwrap_or_else(|| "download".to_string())
}

fn free_name(dir: &std::path::Path, name: &str) -> PathBuf {
    let first = dir.join(name);
    if !first.exists() {
        return first;
    }
    let (stem, ext) = match name.rfind('.') {
        Some(i) if i > 0 => (&name[..i], &name[i..]),
        _ => (name, ""),
    };
    (2..1000)
        .map(|n| dir.join(format!("{stem} ({n}){ext}")))
        .find(|p| !p.exists())
        .unwrap_or(first)
}

fn close_splash(handle: &AppHandle) {
    if let Some(splash) = handle.get_webview_window("splash") {
        let _ = splash.close();
    }
}

fn focus_main(handle: &AppHandle) {
    if let Some(window) = handle.get_webview_window("main") {
        let _ = window.unminimize();
        let _ = window.show();
        let _ = window.set_focus();
    }
}

fn open_main(handle: &AppHandle, port: u16, token: &str) -> tauri::Result<()> {
    let url: Url = format!("http://127.0.0.1:{port}/?token={token}")
        .parse()
        .expect("loopback url");
    let (width, height) = handle
        .primary_monitor()
        .ok()
        .flatten()
        .map(|m| {
            let size = m.size();
            let scale = m.scale_factor();
            (
                (size.width as f64 / scale * SCREEN_FRACTION).max(MIN_LOGICAL.0),
                (size.height as f64 / scale * SCREEN_FRACTION).max(MIN_LOGICAL.1),
            )
        })
        .unwrap_or(FALLBACK_LOGICAL);
    let nav = handle.clone();
    let popup = handle.clone();
    let downloads = handle
        .path()
        .download_dir()
        .unwrap_or_else(|_| data_dir(handle));
    let finished = handle.clone();
    let builder = WebviewWindowBuilder::new(handle, "main", WebviewUrl::External(url))
        .title("formNET")
        .visible(reveal::BUILD_VISIBLE)
        .background_color(reveal::BACKGROUND)
        .inner_size(width, height)
        .min_inner_size(MIN_LOGICAL.0, MIN_LOGICAL.1)
        .center()
        .disable_drag_drop_handler()
        .on_navigation(move |url| {
            if is_local(url, port) || url.scheme() == "about" {
                return true;
            }
            open_outside(&nav, url);
            false
        })
        .on_new_window(move |url, _features| {
            open_outside(&popup, &url);
            NewWindowResponse::Deny
        })
        .on_download(move |_webview, event| match event {
            DownloadEvent::Requested { url, destination } => {
                if !is_local(&url, port) {
                    return false;
                }
                let name = destination
                    .file_name()
                    .map(|n| n.to_string_lossy().into_owned())
                    .filter(|n| !n.is_empty())
                    .unwrap_or_else(|| file_name(&url));
                let _ = std::fs::create_dir_all(&downloads);
                *destination = free_name(&downloads, &name);
                true
            }
            DownloadEvent::Finished { path, success, .. } => {
                if success {
                    if let Some(path) = path {
                        let _ = finished.opener().reveal_item_in_dir(path);
                    }
                }
                true
            }
            _ => true,
        });
    #[cfg(target_os = "macos")]
    let builder = builder
        .title_bar_style(tauri::TitleBarStyle::Overlay)
        .hidden_title(true);
    let window = builder
        .on_page_load(|window, payload| {
            if matches!(payload.event(), tauri::webview::PageLoadEvent::Finished) {
                traffic_lights::place(&window.as_ref().window());
            }
        })
        .build()?;
    reveal::conceal(&window);
    traffic_lights::place(&window.as_ref().window());
    Ok(())
}

fn fail(handle: &AppHandle, text: &str) {
    close_splash(handle);
    let quit = handle.clone();
    handle
        .dialog()
        .message(text)
        .title("formNET")
        .show(move |_| quit.exit(1));
}

fn start(handle: AppHandle) {
    let dir = data_dir(&handle);
    if let Err(err) = std::fs::create_dir_all(&dir) {
        fail(&handle, &format!("formNET could not create its data folder {}: {err}", dir.display()));
        return;
    }
    let token = desktop_token();
    let spawned = handle
        .shell()
        .sidecar("formnet-server")
        .map(|cmd| {
            cmd.args(["serve"])
                .env("FORMNET_DESKTOP", "1")
                .env("FORMNET_TOKEN", &token)
                .env("FORMNET_DATA", dir.as_os_str())
                .env("FORMNET_ADDR", PREFERRED_ADDR)
                .spawn()
        });
    let (mut rx, child) = match spawned {
        Ok(Ok(pair)) => pair,
        Ok(Err(err)) | Err(err) => {
            fail(&handle, &format!("The formNET server could not start: {err}"));
            return;
        }
    };
    let server = handle.state::<Server>();
    server.child.lock().unwrap().replace(child);
    let stopped = server.stopped.clone();
    tauri::async_runtime::spawn(async move {
        let mut port: Option<u16> = None;
        let mut errors: Vec<String> = Vec::new();
        while let Some(event) = rx.recv().await {
            match event {
                CommandEvent::Stdout(bytes) if port.is_none() => {
                    let line = String::from_utf8_lossy(&bytes);
                    if let Some(rest) = line.trim().strip_prefix(READY) {
                        if let Ok(found) = rest.trim_end_matches('/').parse::<u16>() {
                            port = Some(found);
                            match open_main(&handle, found, &token) {
                                Ok(()) => {}
                                Err(err) => {
                                    fail(&handle, &format!("formNET could not open its window: {err}"));
                                    return;
                                }
                            }
                        }
                    }
                }
                CommandEvent::Stderr(bytes) if port.is_none() => {
                    let line = String::from_utf8_lossy(&bytes).trim().to_string();
                    if line.contains("level=ERROR") {
                        errors.push(line);
                    }
                }
                CommandEvent::Terminated(_) => {
                    stopped.store(true, Ordering::SeqCst);
                    if handle.state::<Server>().quitting.load(Ordering::SeqCst) {
                        return;
                    }
                    let detail = errors.last().cloned().unwrap_or_default();
                    let text = if port.is_none() {
                        format!("The formNET server stopped before it was ready.\n\n{detail}")
                    } else {
                        "The formNET server stopped unexpectedly. Open formNET again to continue; recordings are kept in memory only, so this one is gone.".to_string()
                    };
                    fail(&handle, text.trim());
                    return;
                }
                _ => {}
            }
        }
    });
}

fn stop_server(handle: &AppHandle) {
    let server = handle.state::<Server>();
    server.quitting.store(true, Ordering::SeqCst);
    let Some(child) = server.child.lock().unwrap().take() else {
        return;
    };
    let pid = child.pid();
    drop(child);
    let started = Instant::now();
    while !server.stopped.load(Ordering::SeqCst) && started.elapsed() < STOP_WAIT {
        std::thread::sleep(Duration::from_millis(50));
    }
    if !server.stopped.load(Ordering::SeqCst) {
        #[cfg(unix)]
        let _ = std::process::Command::new("kill").args(["-KILL", &pid.to_string()]).status();
        #[cfg(windows)]
        let _ = std::process::Command::new("taskkill").args(["/PID", &pid.to_string(), "/F"]).status();
    }
}

fn main() {
    reveal::mark_launch();
    let app = tauri::Builder::default()
        .plugin(tauri_plugin_single_instance::init(|app, _argv, _cwd| focus_main(app)))
        .plugin(tauri_plugin_shell::init())
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_opener::init())
        .manage(Server {
            child: Mutex::new(None),
            stopped: Arc::new(AtomicBool::new(false)),
            quitting: AtomicBool::new(false),
        })
        .manage(zoom::Zoom(Mutex::new(1.0)))
        .on_window_event(|window, event| {
            if window.label() == "main"
                && matches!(
                    event,
                    WindowEvent::Resized(_) | WindowEvent::Focused(_) | WindowEvent::ThemeChanged(_) | WindowEvent::ScaleFactorChanged { .. }
                )
            {
                traffic_lights::place(window);
            }
        })
        .invoke_handler(tauri::generate_handler![
            traffic_lights::set_window_title,
            reveal::app_ready,
            text_edit::text_edit,
            zoom::ui_zoom
        ])
        .setup(|app| {
            let handle = app.handle().clone();
            reveal::round_splash(&handle);
            start(handle);
            Ok(())
        })
        .build(tauri::generate_context!())
        .expect("the formNET desktop app failed to build");

    app.run(|handle, event| {
        if let RunEvent::Exit = event {
            stop_server(handle);
        }
    });
}
