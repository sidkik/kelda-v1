/*
The sync package implements Kelda's sync algorithm. It handles syncing local
files to a staging location within the container, and syncing from the staging
files to the source final destination.

There are three types of files:
1) SourceFiles -- These are files that exist on the user's machine.
2) MirrorFiles -- These files keep mirror the SourceFiles in the container.
   This way, the sync to DestinationFiles occurs on the local filesystem, so
   the length of the "in between" state won't be impacted by network latency
   between the user's machine and the container.
3) DestinationFiles -- These are the final synced files in the container.

The sync server running on the user's laptop is responsible for syncing
SourceFiles to MirrorFiles, as well as sending the SyncConfig to the container.

The container then uses the MirrorFiles and SyncConfig to install the files
into the proper places within the container.

The sync algorithm only deals with files. Empty directories aren't synced.
This lets us cleanly sync files into directories that contain files that
shouldn't be affected by syncing.
*/
package sync
