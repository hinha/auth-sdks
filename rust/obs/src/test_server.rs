//! Minimal HTTP/1.1 capture server for in-process tests. It plays the role of `httptest` in the Go
//! suite: a real TCP listener on an ephemeral port, with each request recorded for assertions.

use std::collections::HashMap;
use std::io::{BufRead, BufReader, Read, Write};
use std::net::{TcpListener, TcpStream};
use std::sync::{Arc, Mutex};
use std::thread;
use std::time::Duration;

/// One request as received by the server. Header names are lowercased. `raw` holds the exact body
/// bytes, which protobuf and snappy payloads need.
#[derive(Debug, Clone)]
pub struct Captured {
    pub path: String,
    pub headers: HashMap<String, String>,
    pub raw: Vec<u8>,
}

/// A running capture server. The listener thread lives until the test process exits.
pub struct TestServer {
    pub url: String,
    captured: Arc<Mutex<Vec<Captured>>>,
}

impl TestServer {
    /// Answers request `i` with `statuses[i]`. The last status repeats. Status `0` holds the
    /// connection open without replying, which exercises client timeouts.
    pub fn start(statuses: Vec<u16>) -> TestServer {
        let listener = TcpListener::bind("127.0.0.1:0").expect("bind loopback");
        let addr = listener.local_addr().expect("local addr");
        let captured = Arc::new(Mutex::new(Vec::new()));
        let shared = Arc::clone(&captured);
        thread::spawn(move || {
            for (i, stream) in listener.incoming().enumerate() {
                let Ok(stream) = stream else { break };
                let status = statuses.get(i).or(statuses.last()).copied().unwrap_or(204);
                serve_one(stream, status, &shared);
            }
        });
        TestServer {
            url: format!("http://{addr}"),
            captured,
        }
    }

    /// Requests received so far, in arrival order.
    pub fn requests(&self) -> Vec<Captured> {
        self.captured.lock().expect("captured lock").clone()
    }
}

fn serve_one(stream: TcpStream, status: u16, captured: &Mutex<Vec<Captured>>) {
    let mut reader = BufReader::new(stream);
    let mut request_line = String::new();
    if reader.read_line(&mut request_line).unwrap_or(0) == 0 {
        return;
    }
    let mut parts = request_line.split_whitespace();
    let path = parts.nth(1).unwrap_or_default().to_string();

    let mut headers = HashMap::new();
    let mut content_length = 0usize;
    loop {
        let mut line = String::new();
        if reader.read_line(&mut line).unwrap_or(0) == 0 {
            break;
        }
        let line = line.trim_end();
        if line.is_empty() {
            break;
        }
        if let Some((name, value)) = line.split_once(':') {
            let name = name.trim().to_ascii_lowercase();
            let value = value.trim().to_string();
            if name == "content-length" {
                content_length = value.parse().unwrap_or(0);
            }
            headers.insert(name, value);
        }
    }
    let mut body = vec![0u8; content_length];
    let _ = reader.read_exact(&mut body);

    captured.lock().expect("captured lock").push(Captured {
        path,
        headers,
        raw: body,
    });

    let mut stream = reader.into_inner();
    if status == 0 {
        thread::sleep(Duration::from_secs(2));
        return;
    }
    let _ = write!(
        stream,
        "HTTP/1.1 {status} Test\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"
    );
    let _ = stream.flush();
}
