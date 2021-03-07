--
-- PostgreSQL database dump
--

-- Dumped from database version 10.6
-- Dumped by pg_dump version 11.4

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_with_oids = false;

--
-- Name: analytics; Type: TABLE; Schema: public; Owner: analytics
--

CREATE TABLE public.analytics (
    id integer NOT NULL,
    "time" timestamp without time zone,
    customer text,
    namespace text,
    event text,
    additional jsonb
);


ALTER TABLE public.analytics OWNER TO analytics;

--
-- Name: analytics_id_seq; Type: SEQUENCE; Schema: public; Owner: analytics
--

CREATE SEQUENCE public.analytics_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER TABLE public.analytics_id_seq OWNER TO analytics;

--
-- Name: analytics_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: analytics
--

ALTER SEQUENCE public.analytics_id_seq OWNED BY public.analytics.id;


--
-- Name: commands_run; Type: VIEW; Schema: public; Owner: analytics
--

CREATE VIEW public.commands_run AS
 SELECT analytics."time",
    analytics.customer,
    analytics.namespace,
    analytics.event,
    (analytics.additional ->> 'cmd'::text) AS cmd
   FROM public.analytics
  WHERE ((analytics.additional -> 'cmd'::text) IS NOT NULL);


ALTER TABLE public.commands_run OWNER TO analytics;

--
-- Name: errors; Type: VIEW; Schema: public; Owner: analytics
--

CREATE VIEW public.errors AS
 SELECT analytics."time",
    analytics.customer,
    analytics.namespace,
    (analytics.additional ->> 'logrus-message'::text) AS error,
    analytics.additional
   FROM public.analytics
  WHERE (analytics.event ~~ 'Logrus error'::text);


ALTER TABLE public.errors OWNER TO analytics;

--
-- Name: namespaces; Type: VIEW; Schema: public; Owner: analytics
--

CREATE VIEW public.namespaces AS
 SELECT analytics."time",
    analytics.customer,
    analytics.namespace
   FROM public.analytics
  ORDER BY analytics."time" DESC;


ALTER TABLE public.namespaces OWNER TO analytics;

--
-- Name: versions; Type: VIEW; Schema: public; Owner: analytics
--

CREATE VIEW public.versions AS
 SELECT analytics."time",
    analytics.customer,
    analytics.namespace,
    (analytics.additional ->> 'kelda-binary-version'::text) AS kelda_version
   FROM public.analytics
  WHERE ((analytics.additional ->> 'kelda-binary-version'::text) IS NOT NULL);


ALTER TABLE public.versions OWNER TO analytics;

--
-- Name: analytics id; Type: DEFAULT; Schema: public; Owner: analytics
--

ALTER TABLE ONLY public.analytics ALTER COLUMN id SET DEFAULT nextval('public.analytics_id_seq'::regclass);


--
-- Name: analytics analytics_pkey; Type: CONSTRAINT; Schema: public; Owner: analytics
--

ALTER TABLE ONLY public.analytics
    ADD CONSTRAINT analytics_pkey PRIMARY KEY (id);


--
-- Name: SCHEMA public; Type: ACL; Schema: -; Owner: analytics
--

REVOKE ALL ON SCHEMA public FROM rdsadmin;
REVOKE ALL ON SCHEMA public FROM PUBLIC;
GRANT ALL ON SCHEMA public TO analytics;
GRANT ALL ON SCHEMA public TO PUBLIC;


--
-- PostgreSQL database dump complete
--
