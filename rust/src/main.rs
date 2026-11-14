use std::env;
use std::process::ExitCode;

use template_json_math::checked_add;

fn main() -> ExitCode {
    match run(env::args().skip(1).collect()) {
        Ok(result) => {
            println!(r#"{{"result":{result}}}"#);
            ExitCode::SUCCESS
        }
        Err(message) => {
            println!(r#"{{"error":"{message}"}}"#);
            ExitCode::from(2)
        }
    }
}

fn run(args: Vec<String>) -> Result<i64, &'static str> {
    if args.len() != 3 || args[0] != "add" {
        return Err("usage: json_math add <left> <right>");
    }
    let left = args[1]
        .parse::<i64>()
        .map_err(|_| "left must be an integer")?;
    let right = args[2]
        .parse::<i64>()
        .map_err(|_| "right must be an integer")?;
    checked_add(left, right)
}

#[cfg(test)]
mod tests {
    use super::run;

    #[test]
    fn parses_add_request() {
        assert_eq!(run(vec!["add".into(), "20".into(), "22".into()]), Ok(42));
    }

    #[test]
    fn rejects_bad_arguments() {
        assert!(run(vec!["multiply".into(), "6".into(), "7".into()]).is_err());
    }
}
