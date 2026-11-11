-- A domain or category created through the v1 screens gets a node.
--
-- Migration 20 backfilled what existed, but nothing kept the two models in step
-- afterwards: a category created in the console today has no node, so its questions
-- have no node either, and migration 26's mirror cannot place them. The tree would
-- have quietly stopped being the whole picture.
--
-- This is the other half of AD-2. While both models exist, writing to either keeps
-- the other true — questions already do it (migration 21), and now so does the
-- catalog they hang from. All of it goes with CUT-4.

CREATE FUNCTION domains_make_node() RETURNS trigger AS $$
DECLARE new_node bigint;
BEGIN
  IF NEW.node_id IS NOT NULL THEN
    RETURN NEW;
  END IF;
  INSERT INTO nodes (slug, node_type, visibility, status, icon, color, motif, position, metadata)
  VALUES (NEW.slug, 'domain',
          CASE WHEN NEW.is_published THEN 'public' ELSE 'private' END::node_visibility,
          CASE WHEN NEW.is_published THEN 'published' ELSE 'draft' END::node_status,
          NEW.icon, NEW.color, NEW.motif, NEW.position,
          jsonb_build_object('family', NEW.family, 'legacy_domain_id', NEW.id))
  RETURNING id INTO new_node;
  NEW.node_id := new_node;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE FUNCTION categories_make_node() RETURNS trigger AS $$
DECLARE new_node bigint; parent_node bigint;
BEGIN
  IF NEW.node_id IS NOT NULL THEN
    RETURN NEW;
  END IF;
  SELECT d.node_id INTO parent_node FROM domains d WHERE d.id = NEW.domain_id;
  INSERT INTO nodes (parent_id, slug, node_type, visibility, status, icon, color, position, metadata)
  VALUES (parent_node, NEW.slug, 'category',
          CASE WHEN NEW.is_published THEN 'public' ELSE 'private' END::node_visibility,
          CASE WHEN NEW.is_published THEN 'published' ELSE 'draft' END::node_status,
          NEW.icon, NEW.color, NEW.position,
          jsonb_build_object('legacy_category_id', NEW.id))
  RETURNING id INTO new_node;
  NEW.node_id := new_node;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- BEFORE INSERT: identity columns are filled by then, so the node can record which
-- row it stands for and the row can record its node in one pass.
CREATE TRIGGER domains_create_node BEFORE INSERT ON domains
  FOR EACH ROW EXECUTE FUNCTION domains_make_node();
CREATE TRIGGER categories_create_node BEFORE INSERT ON categories
  FOR EACH ROW EXECUTE FUNCTION categories_make_node();

-- Anything created between migration 20 and this one.
DO $$
DECLARE r record; new_node bigint;
BEGIN
  FOR r IN SELECT * FROM domains WHERE node_id IS NULL LOOP
    INSERT INTO nodes (slug, node_type, visibility, status, icon, color, motif, position, metadata)
    VALUES (r.slug, 'domain',
            CASE WHEN r.is_published THEN 'public' ELSE 'private' END::node_visibility,
            CASE WHEN r.is_published THEN 'published' ELSE 'draft' END::node_status,
            r.icon, r.color, r.motif, r.position,
            jsonb_build_object('family', r.family, 'legacy_domain_id', r.id))
    RETURNING id INTO new_node;
    UPDATE domains SET node_id = new_node WHERE id = r.id;
  END LOOP;
  FOR r IN SELECT * FROM categories WHERE node_id IS NULL LOOP
    INSERT INTO nodes (parent_id, slug, node_type, visibility, status, icon, color, position, metadata)
    VALUES ((SELECT node_id FROM domains WHERE id = r.domain_id), r.slug, 'category',
            CASE WHEN r.is_published THEN 'public' ELSE 'private' END::node_visibility,
            CASE WHEN r.is_published THEN 'published' ELSE 'draft' END::node_status,
            r.icon, r.color, r.position,
            jsonb_build_object('legacy_category_id', r.id))
    RETURNING id INTO new_node;
    UPDATE categories SET node_id = new_node WHERE id = r.id;
  END LOOP;
END $$;
