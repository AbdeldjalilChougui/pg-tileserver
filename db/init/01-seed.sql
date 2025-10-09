-- Seed data for pg-tileserver. Coordinates are approximate city centres
-- typed by hand (no external dataset). Three layers exercise different
-- geometry types and SRIDs:
--   cities     POINT       SRID 4326  (transformed per tile)
--   corridors  LINESTRING  SRID 4326
--   hexgrid    POLYGON     SRID 3857  (no transform needed)
CREATE EXTENSION IF NOT EXISTS postgis;

CREATE TABLE cities (
  id         serial PRIMARY KEY,
  name       text NOT NULL,
  name_ar    text,
  wilaya     int2,
  population int4,
  capital    bool DEFAULT false,
  geom       geometry(Point, 4326) NOT NULL
);

INSERT INTO cities (name, name_ar, wilaya, population, capital, geom) VALUES
  ('Alger',       'الجزائر',   16, 2988145, true,  ST_SetSRID(ST_MakePoint(3.0588, 36.7538), 4326)),
  ('Oran',        'وهران',     31,  803329, false, ST_SetSRID(ST_MakePoint(-0.6417, 35.6971), 4326)),
  ('Constantine', 'قسنطينة',   25,  448374, false, ST_SetSRID(ST_MakePoint(6.6147, 36.3650), 4326)),
  ('Annaba',      'عنابة',     23,  257359, false, ST_SetSRID(ST_MakePoint(7.7667, 36.9000), 4326)),
  ('Blida',       'البليدة',    9,  182447, false, ST_SetSRID(ST_MakePoint(2.8277, 36.4700), 4326)),
  ('Batna',       'باتنة',      5,  290645, false, ST_SetSRID(ST_MakePoint(6.1742, 35.5560), 4326)),
  ('Sétif',       'سطيف',      19,  288461, false, ST_SetSRID(ST_MakePoint(5.4108, 36.1911), 4326)),
  ('Tlemcen',     'تلمسان',    13,  140158, false, ST_SetSRID(ST_MakePoint(-1.3150, 34.8783), 4326)),
  ('Béjaïa',      'بجاية',      6,  177988, false, ST_SetSRID(ST_MakePoint(5.0567, 36.7509), 4326)),
  ('Biskra',      'بسكرة',      7,  204661, false, ST_SetSRID(ST_MakePoint(5.7333, 34.8500), 4326)),
  ('Ghardaïa',    'غرداية',    47,   93423, false, ST_SetSRID(ST_MakePoint(3.6739, 32.4911), 4326)),
  ('Ouargla',     'ورقلة',     30,  133024, false, ST_SetSRID(ST_MakePoint(5.3250, 31.9539), 4326)),
  ('Béchar',      'بشار',       8,  165241, false, ST_SetSRID(ST_MakePoint(-2.2167, 31.6167), 4326)),
  ('Adrar',       'أدرار',      1,   64781, false, ST_SetSRID(ST_MakePoint(-0.2833, 27.8667), 4326)),
  ('Tamanrasset', 'تمنراست',   11,   92635, false, ST_SetSRID(ST_MakePoint(5.5228, 22.7850), 4326)),
  ('Illizi',      'إليزي',     33,   17252, false, ST_SetSRID(ST_MakePoint(8.4833, 26.4833), 4326)),
  ('Tindouf',     'تندوف',     37,   47965, false, ST_SetSRID(ST_MakePoint(-8.1474, 27.6711), 4326));

CREATE INDEX cities_geom_idx ON cities USING gist (geom);

-- Straight "corridors" between cities (illustrative, not real roads).
CREATE TABLE corridors (
  gid  serial PRIMARY KEY,
  name text,
  geom geometry(LineString, 4326)
);
INSERT INTO corridors (name, geom)
SELECT a.name || ' – ' || b.name, ST_MakeLine(a.geom, b.geom)
FROM cities a JOIN cities b ON (a.name, b.name) IN (
  ('Oran', 'Alger'), ('Alger', 'Constantine'), ('Constantine', 'Annaba'),
  ('Alger', 'Ghardaïa'), ('Ghardaïa', 'Tamanrasset'), ('Oran', 'Béchar'),
  ('Béchar', 'Adrar'), ('Biskra', 'Ouargla'), ('Batna', 'Biskra'), ('Tlemcen', 'Oran'));
CREATE INDEX corridors_geom_idx ON corridors USING gist (geom);

-- A 75 km hexagon grid over Algeria's bounding box, stored in Web Mercator,
-- with the number of seed cities in each cell.
CREATE TABLE hexgrid (
  id     serial PRIMARY KEY,
  cities int4,
  geom   geometry(Polygon, 3857)
);
INSERT INTO hexgrid (cities, geom)
SELECT (SELECT count(*) FROM cities c WHERE ST_Intersects(ST_Transform(c.geom, 3857), h.geom)), h.geom
FROM ST_HexagonGrid(75000, ST_Transform(ST_MakeEnvelope(-8.7, 19.0, 12.0, 37.1, 4326), 3857)) AS h;
CREATE INDEX hexgrid_geom_idx ON hexgrid USING gist (geom);

ANALYZE;
