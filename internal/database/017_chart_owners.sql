-- Charts get Fanmade's owner collection and co-owner invitations (037_chart_owners.sql):
-- uploader_id becomes owner_ids, a nonempty, duplicate-free array.
-- The charts once imported from ESE belonged to the all-zero system user; they and its
-- upload receipts now belong to the account 2737049640d1477fa4828eb9538ec8b8, and the
-- system user goes (it never signed in, voted, commented or made anything else).
INSERT INTO users(id) SELECT '2737049640d1477fa4828eb9538ec8b8'
 WHERE EXISTS (SELECT 1 FROM users WHERE id = '00000000000000000000000000000000')
 ON CONFLICT DO NOTHING;
ALTER TABLE charts DISABLE TRIGGER USER;
UPDATE charts SET uploader_id = '2737049640d1477fa4828eb9538ec8b8' WHERE uploader_id = '00000000000000000000000000000000';
ALTER TABLE charts ENABLE TRIGGER USER;
UPDATE internal.upload_requests SET user_id = '2737049640d1477fa4828eb9538ec8b8' WHERE user_id = '00000000000000000000000000000000';
DELETE FROM users WHERE id = '00000000000000000000000000000000';

ALTER TABLE charts DROP CONSTRAINT charts_uploader_id_fkey;
DROP INDEX charts_uploader;
ALTER TABLE charts ALTER COLUMN uploader_id TYPE text[] USING ARRAY[uploader_id];
ALTER TABLE charts RENAME COLUMN uploader_id TO owner_ids;
CREATE FUNCTION valid_chart_owners(ids text[]) RETURNS boolean
LANGUAGE sql IMMUTABLE AS $$
 SELECT COALESCE(array_ndims(ids)=1 AND array_lower(ids,1)=1 AND cardinality(ids)>0
  AND cardinality(ids)=(SELECT count(DISTINCT id) FROM unnest(ids) id WHERE id<>''),false)
$$;
ALTER TABLE charts ADD CONSTRAINT charts_owners_valid CHECK(valid_chart_owners(owner_ids));
CREATE INDEX charts_owners ON charts USING gin(owner_ids);
-- PostgreSQL does not support foreign keys on array elements. Preserve the
-- old reference protection, including concurrent user deletion, with row locks.
CREATE FUNCTION check_chart_owners() RETURNS trigger LANGUAGE plpgsql SET search_path FROM CURRENT AS $$
DECLARE owner text;
BEGIN
 FOREACH owner IN ARRAY NEW.owner_ids LOOP
  PERFORM id FROM users WHERE id=owner FOR KEY SHARE;
  IF NOT FOUND THEN RAISE EXCEPTION 'chart owner does not exist' USING ERRCODE='23503'; END IF;
 END LOOP;
 RETURN NEW;
END;
$$;
CREATE TRIGGER charts_check_owners BEFORE INSERT OR UPDATE OF owner_ids ON charts
 FOR EACH ROW EXECUTE FUNCTION check_chart_owners();
CREATE FUNCTION protect_chart_owner() RETURNS trigger LANGUAGE plpgsql SET search_path FROM CURRENT AS $$
BEGIN
 IF TG_OP='UPDATE' AND NEW.id=OLD.id THEN RETURN NEW; END IF;
 IF EXISTS(SELECT 1 FROM charts WHERE owner_ids @> ARRAY[OLD.id]) THEN
  RAISE EXCEPTION 'user owns charts' USING ERRCODE='23503';
 END IF;
 IF TG_OP='DELETE' THEN RETURN OLD; END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER users_protect_chart_owner BEFORE DELETE OR UPDATE OF id ON users
 FOR EACH ROW EXECUTE FUNCTION protect_chart_owner();

CREATE TABLE chart_collaborations (
 id text PRIMARY KEY,
 chart_id text NOT NULL REFERENCES charts(id) ON DELETE CASCADE,
 inviter_id text NOT NULL REFERENCES users(id),
 recipient_id text NOT NULL REFERENCES users(id),
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','accepted','declined')),
 created_at timestamptz NOT NULL DEFAULT now(),
 responded_at timestamptz,
 CHECK(inviter_id<>recipient_id)
);
CREATE UNIQUE INDEX chart_collaborations_pending ON chart_collaborations(chart_id,recipient_id) WHERE status='pending';
CREATE INDEX chart_collaborations_inviter ON chart_collaborations(inviter_id,created_at);
ALTER TABLE notifications ADD COLUMN collaboration_id text REFERENCES chart_collaborations(id) ON DELETE CASCADE;
CREATE UNIQUE INDEX notifications_collaboration ON notifications(collaboration_id) WHERE collaboration_id IS NOT NULL;

INSERT INTO internal.schema_migrations(version) VALUES (17);
