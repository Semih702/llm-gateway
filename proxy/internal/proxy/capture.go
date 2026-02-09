package proxy

type limitedCapture struct {
	limit int
	buf   []byte
}

func NewLimitedCapture(limit int) *limitedCapture {
	if limit <= 0 {
		return &limitedCapture{limit: 0, buf: nil}
	}
	return &limitedCapture{limit: limit, buf: make([]byte, 0, Min(limit, 16*1024))}
}

func (lc *limitedCapture) Write(p []byte) (int, error) {
	if lc.limit <= 0 {
		return len(p), nil
	}
	remain := lc.limit - len(lc.buf)
	if remain <= 0 {
		return len(p), nil
	}
	if len(p) <= remain {
		lc.buf = append(lc.buf, p...)
		return len(p), nil
	}
	lc.buf = append(lc.buf, p[:remain]...)
	return len(p), nil
}

func (lc *limitedCapture) Bytes() []byte {
	return lc.buf
}
