-- 요청을 받는 컨테이너의 앱 네트워크 IP (설치형은 이 IP로 보낸다). 재시작하면 바뀔 수 있어 사이트 지도가 지금 IP로 덮어쓴다.
ALTER TABLE services ADD COLUMN instance_ip TEXT NOT NULL DEFAULT '';
