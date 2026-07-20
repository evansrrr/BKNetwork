#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use serde::Deserialize;
use std::{
    error::Error,
    io::{Read, Write},
    net::{SocketAddr, TcpStream},
    sync::{
        Arc, Mutex,
        atomic::{AtomicBool, Ordering},
    },
    thread,
    time::{Duration, Instant},
};
use tauri::{
    AppHandle, Manager, Runtime, WebviewUrl, WebviewWindowBuilder, WindowEvent,
    menu::{Menu, MenuItem},
    tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent},
    webview::NewWindowResponse,
};
use tauri_plugin_shell::{
    ShellExt,
    process::{CommandChild, CommandEvent},
};

const BACKEND_ADDR: &str = "127.0.0.1:13335";
const BACKEND_URL: &str = "http://127.0.0.1:13335";

struct SidecarState {
    child: Mutex<Option<CommandChild>>,
    stopping: Arc<AtomicBool>,
}

#[derive(Deserialize)]
struct Settings {
    #[serde(rename = "silentStart")]
    silent_start: bool,
}

fn main() {
    let app = tauri::Builder::default()
        // Single-instance must be the first plugin so a second launch only
        // focuses the existing window and never starts another sidecar.
        .plugin(tauri_plugin_single_instance::init(|app, _args, _cwd| {
            show_main_window(app);
        }))
        .plugin(tauri_plugin_shell::init())
        .setup(|app| {
            let api_token = create_api_token()?;
            start_backend(app, &api_token)?;
            let setup_result = (|| -> Result<(), Box<dyn Error>> {
                wait_for_backend(Duration::from_secs(10))?;
                let start_hidden = read_settings(&api_token)?.silent_start;
                create_main_window(app, start_hidden, &api_token)?;
                create_tray(app)?;
                Ok(())
            })();
            if setup_result.is_err() {
                stop_backend(app.handle());
            }
            setup_result
        })
        .on_menu_event(|app, event| match event.id().as_ref() {
            "show" => show_main_window(app),
            "quit" => app.exit(0),
            _ => {}
        })
        .on_tray_icon_event(|app, event| {
            if let TrayIconEvent::Click {
                button: MouseButton::Left,
                button_state: MouseButtonState::Up,
                ..
            } = event
            {
                show_main_window(app);
            }
        })
        .on_window_event(|window, event| {
            if let WindowEvent::CloseRequested { api, .. } = event {
                api.prevent_close();
                let _ = window.hide();
            }
        })
        .build(tauri::generate_context!())
        .expect("failed to build BKNetwork");

    app.run(|app, event| {
        if let tauri::RunEvent::Exit = event {
            stop_backend(app);
        }
    });
}

fn create_api_token() -> Result<String, Box<dyn Error>> {
    let mut bytes = [0_u8; 32];
    getrandom::fill(&mut bytes).map_err(|error| {
        std::io::Error::other(format!("random token generation failed: {error}"))
    })?;
    Ok(bytes.iter().map(|byte| format!("{byte:02x}")).collect())
}

fn start_backend(app: &tauri::App, api_token: &str) -> Result<(), Box<dyn Error>> {
    let app_executable = std::env::current_exe()?;
    let command = app
        .shell()
        .sidecar("bknetwork-server")?
        .env("BKNETWORK_APP_EXECUTABLE", app_executable)
        .env("BKNETWORK_API_TOKEN", api_token);
    let (mut events, child) = command.spawn()?;

    let stopping = Arc::new(AtomicBool::new(false));
    app.manage(SidecarState {
        child: Mutex::new(Some(child)),
        stopping: stopping.clone(),
    });

    let app_handle = app.handle().clone();
    tauri::async_runtime::spawn(async move {
        while let Some(event) = events.recv().await {
            match event {
                CommandEvent::Stdout(line) => {
                    eprintln!("[bknetwork-server] {}", String::from_utf8_lossy(&line));
                }
                CommandEvent::Stderr(line) => {
                    eprintln!(
                        "[bknetwork-server:error] {}",
                        String::from_utf8_lossy(&line)
                    );
                }
                CommandEvent::Terminated(status) => {
                    eprintln!("bknetwork-server exited: {status:?}");
                    if !stopping.load(Ordering::Acquire) {
                        app_handle.exit(1);
                    }
                    break;
                }
                CommandEvent::Error(error) => {
                    eprintln!("bknetwork-server error: {error}");
                }
                _ => {}
            }
        }
    });

    Ok(())
}

fn wait_for_backend(timeout: Duration) -> Result<(), Box<dyn Error>> {
    let address: SocketAddr = BACKEND_ADDR.parse()?;
    let deadline = Instant::now() + timeout;

    while Instant::now() < deadline {
        if TcpStream::connect_timeout(&address, Duration::from_millis(250)).is_ok() {
            return Ok(());
        }
        thread::sleep(Duration::from_millis(100));
    }

    Err(format!("BKNetwork backend did not start within {timeout:?}").into())
}

fn read_settings(api_token: &str) -> Result<Settings, Box<dyn Error>> {
    let mut stream = TcpStream::connect(BACKEND_ADDR)?;
    stream.set_read_timeout(Some(Duration::from_secs(2)))?;
    write!(
        stream,
        "GET /api/v1/settings HTTP/1.0\r\nHost: 127.0.0.1:13335\r\nAuthorization: Bearer {api_token}\r\n\r\n"
    )?;

    let mut response = String::new();
    stream.read_to_string(&mut response)?;
    let (headers, body) = response
        .split_once("\r\n\r\n")
        .ok_or("invalid settings response")?;
    let status = headers
        .lines()
        .next()
        .and_then(|line| line.split_whitespace().nth(1))
        .ok_or("invalid settings response status")?;
    if status != "200" {
        return Err(format!("settings request returned HTTP {status}").into());
    }
    Ok(serde_json::from_str(body.trim())?)
}

fn create_main_window(
    app: &tauri::App,
    start_hidden: bool,
    api_token: &str,
) -> Result<(), Box<dyn Error>> {
    let url = format!("{BACKEND_URL}/?token={api_token}").parse()?;

    WebviewWindowBuilder::new(app, "main", WebviewUrl::External(url))
        .title("BKNetwork")
        .inner_size(1180.0, 820.0)
        .min_inner_size(900.0, 650.0)
        .visible(!start_hidden)
        .on_navigation(|url| {
            if is_backend_url(url) {
                return true;
            }
            open_external_url(url.as_str());
            false
        })
        .on_new_window(|url, _features| {
            open_external_url(url.as_str());
            NewWindowResponse::Deny
        })
        .build()?;

    Ok(())
}

fn is_backend_url(url: &tauri::Url) -> bool {
    url.scheme() == "http"
        && matches!(url.host_str(), Some("127.0.0.1") | Some("localhost"))
        && url.port_or_known_default() == Some(13335)
}

fn open_external_url(url: &str) {
    if url.starts_with("http://") || url.starts_with("https://") {
        let _ = tauri_plugin_opener::open_url(url, None::<&str>);
    }
}

fn create_tray(app: &tauri::App) -> tauri::Result<()> {
    let show = MenuItem::with_id(app, "show", "打开 BKNetwork", true, None::<&str>)?;
    let quit = MenuItem::with_id(app, "quit", "退出", true, None::<&str>)?;
    let menu = Menu::with_items(app, &[&show, &quit])?;

    let mut tray = TrayIconBuilder::new()
        .tooltip("BKNetwork")
        .menu(&menu)
        .show_menu_on_left_click(false);
    if let Some(icon) = app.default_window_icon() {
        tray = tray.icon(icon.clone());
    }
    tray.build(app)?;

    Ok(())
}

fn show_main_window<R: Runtime>(app: &AppHandle<R>) {
    if let Some(window) = app.get_webview_window("main") {
        let _ = window.unminimize();
        let _ = window.show();
        let _ = window.set_focus();
    }
}

fn stop_backend<R: Runtime>(app: &AppHandle<R>) {
    let Some(state) = app.try_state::<SidecarState>() else {
        return;
    };
    state.stopping.store(true, Ordering::Release);
    let Ok(mut child) = state.child.lock() else {
        return;
    };
    if let Some(child) = child.take() {
        let _ = child.kill();
    }
}
