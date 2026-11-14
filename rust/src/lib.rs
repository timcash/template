/// Add two signed integers without allowing overflow.
pub fn checked_add(left: i64, right: i64) -> Result<i64, &'static str> {
    left.checked_add(right).ok_or("integer overflow")
}

#[cfg(test)]
mod tests {
    use super::checked_add;

    #[test]
    fn adds_positive_and_negative_integers() {
        assert_eq!(checked_add(40, 2), Ok(42));
        assert_eq!(checked_add(-8, 3), Ok(-5));
    }

    #[test]
    fn reports_overflow() {
        assert_eq!(checked_add(i64::MAX, 1), Err("integer overflow"));
    }
}
