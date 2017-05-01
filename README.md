Ratcam - WIP
=====

simple webcam security app that takes pictures and detects movement.

requires [fswebcam](http://www.firestorm.cx/fswebcam/) and [imagemagick](http://www.imagemagick.org)

tested on a rapsberry pi with arch linux installed

bugs

imagemagick's compare function exits and returns the diff number instead of returning 0 and sending it to stdout.


to concat all the jpgs into an avi

mencoder mf://*.jpg -mf w=480:h=640:fps=4:type=jpg -ovc lavc -lavcopts vcodec=mpeg4:mbd=2:trell -oac copy -o output.avi
