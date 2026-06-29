use anyhow::{Result, anyhow};

pub fn decode(input: &str) -> Result<Vec<u8>> {
    let cleaned: Vec<u8> = input
        .bytes()
        .filter(|b| !matches!(b, b'\n' | b'\r' | b' ' | b'\t'))
        .collect();
    if !cleaned.len().is_multiple_of(4) {
        return Err(anyhow!(
            "base64 length {} is not a multiple of 4",
            cleaned.len()
        ));
    }
    let mut out = Vec::with_capacity(cleaned.len() / 4 * 3);
    for (chunk_idx, chunk) in cleaned.chunks(4).enumerate() {
        let base = chunk_idx * 4;
        let v0 = val(chunk[0], base)?;
        let v1 = val(chunk[1], base + 1)?;
        let v2_opt = if chunk[2] == b'=' {
            None
        } else {
            Some(val(chunk[2], base + 2)?)
        };
        let v3_opt = if chunk[3] == b'=' {
            None
        } else {
            Some(val(chunk[3], base + 3)?)
        };
        out.push((v0 << 2) | (v1 >> 4));
        if let Some(v2) = v2_opt {
            out.push(((v1 & 0x0f) << 4) | (v2 >> 2));
            if let Some(v3) = v3_opt {
                out.push(((v2 & 0x03) << 6) | v3);
            }
        }
    }
    Ok(out)
}

fn val(c: u8, pos: usize) -> Result<u8> {
    match c {
        b'A'..=b'Z' => Ok(c - b'A'),
        b'a'..=b'z' => Ok(c - b'a' + 26),
        b'0'..=b'9' => Ok(c - b'0' + 52),
        b'+' => Ok(62),
        b'/' => Ok(63),
        _ => Err(anyhow!("invalid base64 char 0x{c:02x} at position {pos}")),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn decodes_known_payload() {
        assert_eq!(decode("SGVsbG8gV29ybGQh").unwrap(), b"Hello World!");
    }

    #[test]
    fn rejects_invalid_char() {
        let err = decode("AAA!").unwrap_err().to_string();
        assert!(err.contains("position 3"));
    }

    #[test]
    fn rejects_wrong_length() {
        let err = decode("ABC").unwrap_err().to_string();
        assert!(err.contains("multiple of 4"));
    }
}
