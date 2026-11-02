//! View > Zoom In, Zoom Out and Actual Size, and Preferences > Interface > Interface Scale: the web view's own zoom,
//! asked for by the page (ui_zoom with a factor) and read back by it (ui_zoom without one). The factor is kept for the
//! window's life; the page keeps the chosen scale on disk and asks again at the next start.

use std::sync::Mutex;

pub struct Zoom(pub Mutex<f64>);

const ZOOM_RANGE: (f64, f64) = (0.5, 3.0);

#[tauri::command]
pub fn ui_zoom(window: tauri::WebviewWindow, zoom: tauri::State<Zoom>, factor: Option<f64>) -> Result<f64, String> {
    let busy = || "the zoom state is busy".to_string();
    let current = *zoom.0.lock().map_err(|_| busy())?;
    let Some(asked) = factor else { return Ok(current) };
    let asked = asked.clamp(ZOOM_RANGE.0, ZOOM_RANGE.1);
    if (asked - current).abs() > 1e-6 {
        window.set_zoom(asked).map_err(|err| err.to_string())?;
        *zoom.0.lock().map_err(|_| busy())? = asked;
    }
    Ok(asked)
}
