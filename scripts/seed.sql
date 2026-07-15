-- Seed data for local testing.
-- Run against the dev DB, e.g.:
--   docker exec -i postgres-pea psql -U pea_user -d Test_PEA_DB < scripts/seed.sql
-- Safe to re-run: uses ON CONFLICT DO NOTHING on every unique key.

INSERT INTO vendors (vendor_name, country, type) VALUES
	('Delta', 'Taiwan', 'charger'),
	('ABB', 'Switzerland', 'charger'),
	('Tesla', 'USA', 'ev'),
	('BYD', 'China', 'ev')
ON CONFLICT (vendor_name, type) DO NOTHING;

-- Same test citizen the mockcitizen CLI's default -pid mints a session for,
-- so a POST from that token round-trips against a real existing row.
INSERT INTO general_infos (pid, first_name, last_name, address, ca, status, created_at, updated_at)
VALUES ('1234567890123', 'ทดสอบ', 'ระบบ', '123 ถนนทดสอบ แขวงทดสอบ เขตทดสอบ กรุงเทพฯ 10900', '123456789012', 'pending', now(), now())
ON CONFLICT (ca) DO NOTHING;

-- image_url/label_image_url store S3 object KEYS (not URLs) — geodrive.pea.co.th
-- (Dell EMC ECS) has no anonymous public read, so reads always go through a
-- presigned URL generated on demand (storage.PresignGet). These keys don't need
-- to point at a real uploaded object just to exercise GET /api/v1/general-info.
INSERT INTO chargers (general_info_id, vendor_id, serial_number, connector_type, kw, image_url, label_image_url, brand, model)
SELECT g.id, v.id, 'SN-TEST-0001', 'Type 2', '7.4',
       'chargers/1234567890123/mock-device.jpg',
       'chargers/1234567890123/mock-label.jpg',
       'Delta', 'AC Mini Plus'
FROM general_infos g, vendors v
WHERE g.pid = '1234567890123' AND v.vendor_name = 'Delta' AND v.type = 'charger'
ON CONFLICT (serial_number) DO NOTHING;

INSERT INTO evs (general_info_id, vendor_id, brand, model, year, battery,
                  charging_period, charging_start_time, charging_finish_time)
SELECT g.id, v.id, 'Tesla', 'Model 3', '2023', '60 kWh',
       'ทุกวัน', '22:00', '06:00'
FROM general_infos g, vendors v
WHERE g.pid = '1234567890123' AND v.vendor_name = 'Tesla' AND v.type = 'ev';
