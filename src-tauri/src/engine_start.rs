use std::collections::VecDeque;
use std::time::Duration;

pub const READY_WAIT: Duration = Duration::from_secs(90);
pub const EXIT_GRACE: Duration = Duration::from_secs(1);
const TAIL_LINES: usize = 12;
const LINE_CHARS: usize = 300;

#[derive(Debug)]
pub enum Failure {
    Data(String),
    Missing,
    NotStarted(String),
    Stopped(Option<i32>),
    TimedOut,
    Window(String),
    Lost,
}

pub struct Tail {
    lines: VecDeque<String>,
    secret: String,
}

impl Tail {
    pub fn new(secret: &str) -> Self {
        Tail { lines: VecDeque::new(), secret: secret.to_string() }
    }

    pub fn push(&mut self, line: &str) {
        if line.contains("token=") || (!self.secret.is_empty() && line.contains(&self.secret)) {
            return;
        }
        let clean: String = line.chars().filter(|c| !c.is_control()).take(LINE_CHARS).collect();
        if clean.trim().is_empty() {
            return;
        }
        if self.lines.len() == TAIL_LINES {
            self.lines.pop_front();
        }
        self.lines.push_back(clean);
    }
}

fn sentence(failure: &Failure) -> String {
    match failure {
        Failure::Data(reason) => format!("FormNET's server did not start: FormNET could not create its data folder. {reason}"),
        Failure::Missing => "FormNET's server did not start: its program is missing from the install. Installing FormNET again puts it back.".to_string(),
        Failure::NotStarted(reason) => format!("FormNET's server did not start: the system would not run it ({reason})."),
        Failure::Stopped(Some(code)) => format!("FormNET's server did not start: it stopped with code {code} before the window could open."),
        Failure::Stopped(None) => "FormNET's server did not start: it stopped before the window could open.".to_string(),
        Failure::TimedOut => format!("FormNET's server did not start: FormNET waited {} seconds for it to say it was ready, and it did not, so FormNET stopped it.", READY_WAIT.as_secs()),
        Failure::Window(reason) => format!("FormNET could not open its window: {reason}"),
        Failure::Lost => "The FormNET server stopped unexpectedly. Open FormNET again to continue; recordings are kept in memory only, so this one is gone.".to_string(),
    }
}

pub fn message(failure: &Failure, tail: &Tail) -> String {
    let mut text = sentence(failure);
    if !tail.lines.is_empty() {
        text.push_str("\n\nThe server's last lines:\n");
        text.push_str(&tail.lines.iter().cloned().collect::<Vec<_>>().join("\n"));
    } else if matches!(failure, Failure::Stopped(_) | Failure::TimedOut | Failure::Lost) {
        text.push_str("\n\nThe server wrote nothing before that.");
    }
    text
}
